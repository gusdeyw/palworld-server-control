package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const reportDateLayout = "2006-01-02"

var (
	reportDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reportHeader      = []string{
		"timestamp",
		"server_online",
		"server_fps",
		"frame_time_ms",
		"players",
		"memory_usage",
		"latency_ms",
		"packet_loss_percent",
		"network_status",
	}
)

type ReportRecord struct {
	At            time.Time
	ServerOnline  bool
	FPS           *float64
	FrameTimeMS   *float64
	Players       *int
	MemoryUsage   string
	LatencyMS     *float64
	PacketLoss    *float64
	NetworkStatus string
}

type ReportSummary struct {
	Date            string     `json:"date"`
	Status          string     `json:"status"`
	Samples         int        `json:"samples"`
	GameSamples     int        `json:"gameSamples"`
	NetworkSamples  int        `json:"networkSamples"`
	OnlinePercent   float64    `json:"onlinePercent"`
	AverageFPS      float64    `json:"averageFps"`
	MinimumFPS      float64    `json:"minimumFps"`
	PeakPlayers     int        `json:"peakPlayers"`
	AverageLatency  float64    `json:"averageLatencyMs"`
	MaximumLatency  float64    `json:"maximumLatencyMs"`
	AverageLoss     float64    `json:"averagePacketLoss"`
	MaximumLoss     float64    `json:"maximumPacketLoss"`
	DegradedSamples int        `json:"degradedSamples"`
	CriticalSamples int        `json:"criticalSamples"`
	FirstSample     *time.Time `json:"firstSample,omitempty"`
	LastSample      *time.Time `json:"lastSample,omitempty"`
	Size            int64      `json:"size"`
}

type HourlyReport struct {
	Hour            string  `json:"hour"`
	Status          string  `json:"status"`
	Samples         int     `json:"samples"`
	GameSamples     int     `json:"gameSamples"`
	NetworkSamples  int     `json:"networkSamples"`
	OnlinePercent   float64 `json:"onlinePercent"`
	AverageFPS      float64 `json:"averageFps"`
	PeakPlayers     int     `json:"peakPlayers"`
	AverageLatency  float64 `json:"averageLatencyMs"`
	MaximumLoss     float64 `json:"maximumPacketLoss"`
	DegradedSamples int     `json:"degradedSamples"`
	CriticalSamples int     `json:"criticalSamples"`
}

type DailyReport struct {
	Summary ReportSummary  `json:"summary"`
	Hours   []HourlyReport `json:"hours"`
}

type ReportStore struct {
	dir           string
	retentionDays int
	location      *time.Location
	mu            sync.Mutex
	lastPruneDate string
}

type reportAccumulator struct {
	status         string
	samples        int
	online         int
	fpsSamples     int
	fpsTotal       float64
	minimumFPS     float64
	peakPlayers    int
	latencySamples int
	latencyTotal   float64
	maximumLatency float64
	lossSamples    int
	lossTotal      float64
	maximumLoss    float64
	degraded       int
	critical       int
	first          *time.Time
	last           *time.Time
}

func NewReportStore(directory string, retentionDays int, timezone string) (*ReportStore, error) {
	resolved, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve report directory: %w", err)
	}
	if retentionDays < 1 {
		retentionDays = 30
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load report timezone %q: %w", timezone, err)
	}
	if err := os.MkdirAll(resolved, 0o750); err != nil {
		return nil, fmt.Errorf("create report directory: %w", err)
	}
	return &ReportStore{
		dir:           resolved,
		retentionDays: retentionDays,
		location:      location,
	}, nil
}

func (s *ReportStore) Append(record ReportRecord) error {
	if record.At.IsZero() {
		record.At = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	localTime := record.At.In(s.location)
	date := localTime.Format(reportDateLayout)
	path := filepath.Join(s.dir, date+".csv")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open daily report: %w", err)
	}

	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return fmt.Errorf("inspect daily report: %w", statErr)
	}
	writer := csv.NewWriter(file)
	if info.Size() == 0 {
		if err := writer.Write(reportHeader); err != nil {
			_ = file.Close()
			return fmt.Errorf("write report header: %w", err)
		}
	}
	row := []string{
		localTime.Format(time.RFC3339Nano),
		strconv.FormatBool(record.ServerOnline),
		formatOptionalFloat(record.FPS),
		formatOptionalFloat(record.FrameTimeMS),
		formatOptionalInt(record.Players),
		record.MemoryUsage,
		formatOptionalFloat(record.LatencyMS),
		formatOptionalFloat(record.PacketLoss),
		normalizeReportStatus(record.NetworkStatus),
	}
	if err := writer.Write(row); err != nil {
		_ = file.Close()
		return fmt.Errorf("write report sample: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush report sample: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close daily report: %w", err)
	}

	if s.lastPruneDate != date {
		if err := s.pruneLocked(localTime); err != nil {
			return err
		}
		s.lastPruneDate = date
	}
	return nil
}

func (s *ReportStore) List() ([]ReportSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	reports := make([]ReportSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".csv")
		if !validReportDate(date) {
			continue
		}
		report, err := s.readLocked(date)
		if err != nil {
			continue
		}
		reports = append(reports, report.Summary)
	}
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Date > reports[j].Date
	})
	return reports, nil
}

func (s *ReportStore) Read(date string) (DailyReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(date)
}

func (s *ReportStore) Open(date string) (*os.File, os.FileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validReportDate(date) {
		return nil, nil, errors.New("invalid report date")
	}
	file, err := os.Open(filepath.Join(s.dir, date+".csv"))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func (s *ReportStore) Timezone() string {
	return s.location.String()
}

func (s *ReportStore) RetentionDays() int {
	return s.retentionDays
}

func (s *ReportStore) readLocked(date string) (DailyReport, error) {
	if !validReportDate(date) {
		return DailyReport{}, errors.New("invalid report date")
	}
	path := filepath.Join(s.dir, date+".csv")
	file, err := os.Open(path)
	if err != nil {
		return DailyReport{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return DailyReport{}, err
	}
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return DailyReport{}, fmt.Errorf("read report header: %w", err)
	}
	if !equalStrings(header, reportHeader) {
		return DailyReport{}, errors.New("unsupported report format")
	}

	var daily reportAccumulator
	hours := make(map[int]*reportAccumulator)
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if len(row) != len(reportHeader) {
			if readErr != nil {
				continue
			}
			continue
		}
		record, err := parseReportRecord(row)
		if err != nil {
			continue
		}
		daily.add(record)
		hour := record.At.In(s.location).Hour()
		if hours[hour] == nil {
			hours[hour] = &reportAccumulator{}
		}
		hours[hour].add(record)
	}

	summary := daily.summary(date)
	summary.Size = info.Size()
	hourly := make([]HourlyReport, 0, len(hours))
	for hour := 0; hour < 24; hour++ {
		accumulator := hours[hour]
		if accumulator == nil {
			continue
		}
		value := accumulator.summary(date)
		hourly = append(hourly, HourlyReport{
			Hour:            fmt.Sprintf("%02d:00", hour),
			Status:          value.Status,
			Samples:         value.Samples,
			GameSamples:     value.GameSamples,
			NetworkSamples:  value.NetworkSamples,
			OnlinePercent:   value.OnlinePercent,
			AverageFPS:      value.AverageFPS,
			PeakPlayers:     value.PeakPlayers,
			AverageLatency:  value.AverageLatency,
			MaximumLoss:     value.MaximumLoss,
			DegradedSamples: value.DegradedSamples,
			CriticalSamples: value.CriticalSamples,
		})
	}
	return DailyReport{Summary: summary, Hours: hourly}, nil
}

func (s *ReportStore) pruneLocked(now time.Time) error {
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location).
		AddDate(0, 0, -(s.retentionDays - 1))
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("inspect report retention: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".csv")
		if !validReportDate(date) {
			continue
		}
		day, err := time.ParseInLocation(reportDateLayout, date, s.location)
		if err != nil || !day.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
			return fmt.Errorf("remove expired report %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (a *reportAccumulator) add(record ReportRecord) {
	a.samples++
	if record.ServerOnline {
		a.online++
	}
	if record.FPS != nil {
		a.fpsSamples++
		a.fpsTotal += *record.FPS
		if a.fpsSamples == 1 || *record.FPS < a.minimumFPS {
			a.minimumFPS = *record.FPS
		}
	}
	if record.Players != nil && *record.Players > a.peakPlayers {
		a.peakPlayers = *record.Players
	}
	if record.LatencyMS != nil {
		a.latencySamples++
		a.latencyTotal += *record.LatencyMS
		if *record.LatencyMS > a.maximumLatency {
			a.maximumLatency = *record.LatencyMS
		}
	}
	if record.PacketLoss != nil {
		a.lossSamples++
		a.lossTotal += *record.PacketLoss
		if *record.PacketLoss > a.maximumLoss {
			a.maximumLoss = *record.PacketLoss
		}
	}
	status := normalizeReportStatus(record.NetworkStatus)
	if a.status == "" || reportStatusRank(status) > reportStatusRank(a.status) {
		a.status = status
	}
	if status == networkStatusDegraded {
		a.degraded++
	}
	if status == networkStatusCritical {
		a.critical++
	}
	at := record.At
	if a.first == nil || at.Before(*a.first) {
		copy := at
		a.first = &copy
	}
	if a.last == nil || at.After(*a.last) {
		copy := at
		a.last = &copy
	}
}

func (a *reportAccumulator) summary(date string) ReportSummary {
	status := a.status
	if status == "" {
		status = networkStatusCollecting
	}
	return ReportSummary{
		Date:            date,
		Status:          status,
		Samples:         a.samples,
		GameSamples:     a.fpsSamples,
		NetworkSamples:  a.lossSamples,
		OnlinePercent:   percentage(a.online, a.samples),
		AverageFPS:      average(a.fpsTotal, a.fpsSamples),
		MinimumFPS:      a.minimumFPS,
		PeakPlayers:     a.peakPlayers,
		AverageLatency:  average(a.latencyTotal, a.latencySamples),
		MaximumLatency:  a.maximumLatency,
		AverageLoss:     average(a.lossTotal, a.lossSamples),
		MaximumLoss:     a.maximumLoss,
		DegradedSamples: a.degraded,
		CriticalSamples: a.critical,
		FirstSample:     a.first,
		LastSample:      a.last,
	}
}

func parseReportRecord(row []string) (ReportRecord, error) {
	at, err := time.Parse(time.RFC3339Nano, row[0])
	if err != nil {
		return ReportRecord{}, err
	}
	online, err := strconv.ParseBool(row[1])
	if err != nil {
		return ReportRecord{}, err
	}
	return ReportRecord{
		At:            at,
		ServerOnline:  online,
		FPS:           parseOptionalFloat(row[2]),
		FrameTimeMS:   parseOptionalFloat(row[3]),
		Players:       parseOptionalInt(row[4]),
		MemoryUsage:   row[5],
		LatencyMS:     parseOptionalFloat(row[6]),
		PacketLoss:    parseOptionalFloat(row[7]),
		NetworkStatus: normalizeReportStatus(row[8]),
	}, nil
}

func validReportDate(date string) bool {
	if !reportDatePattern.MatchString(date) {
		return false
	}
	parsed, err := time.Parse(reportDateLayout, date)
	return err == nil && parsed.Format(reportDateLayout) == date
}

func normalizeReportStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case networkStatusHealthy:
		return networkStatusHealthy
	case networkStatusDegraded:
		return networkStatusDegraded
	case networkStatusCritical:
		return networkStatusCritical
	case networkStatusDisabled:
		return networkStatusDisabled
	default:
		return networkStatusCollecting
	}
}

func reportStatusRank(status string) int {
	switch status {
	case networkStatusCritical:
		return 4
	case networkStatusDegraded:
		return 3
	case networkStatusHealthy:
		return 2
	case networkStatusCollecting:
		return 1
	default:
		return 0
	}
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', 3, 64)
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func parseOptionalFloat(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseOptionalInt(value string) *int {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func percentage(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func average(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
