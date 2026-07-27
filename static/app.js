(() => {
  "use strict";

  const app = {
    state: null,
    history: [],
    activeView: "overview",
    loggedIn: false,
    refreshTimer: null,
    logTimer: null,
    pendingAction: null,
    chartResizeObserver: null,
    settingsData: null,
    settingsDraft: new Map(),
    settingsGroup: "combat",
    settingsSearch: "",
    settingsLoading: false,
    pendingSettingsAction: null,
  };

  const el = (id) => document.getElementById(id);
  const all = (selector, root = document) => [...root.querySelectorAll(selector)];

  async function api(path, options = {}) {
    const method = options.method || "GET";
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (method !== "GET") {
      headers["Content-Type"] = "application/json";
      if (path !== "/api/login") headers["X-Pal-Control"] = "1";
    }
    const response = await fetch(path, {
      credentials: "same-origin",
      ...options,
      method,
      headers,
    });
    let body = {};
    try {
      body = await response.json();
    } catch {
      body = {};
    }
    if (response.status === 401) {
      showLogin();
      throw new Error(body.error || "Sign in required");
    }
    if (!response.ok) {
      throw new Error(body.error || `Request failed with status ${response.status}`);
    }
    return body;
  }

  function showLogin() {
    app.loggedIn = false;
    el("login-layer").hidden = false;
    window.setTimeout(() => el("panel-password").focus(), 50);
  }

  function hideLogin() {
    app.loggedIn = true;
    el("login-layer").hidden = true;
    el("login-error").textContent = "";
  }

  async function loadState({ quiet = false } = {}) {
    const refreshButton = el("refresh-button");
    if (!quiet) {
      refreshButton.disabled = true;
      refreshButton.setAttribute("aria-busy", "true");
    }
    try {
      const state = await api("/api/state");
      hideLogin();
      app.state = state;
      renderState(state);
      await loadHistory();
      if (app.activeView === "utilities") loadBackups();
      if (app.activeView === "settings" && !app.settingsData) loadGameSettings();
    } catch (error) {
      if (app.loggedIn && !quiet) toast(error.message, true);
    } finally {
      refreshButton.disabled = false;
      refreshButton.removeAttribute("aria-busy");
    }
  }

  function renderState(state) {
    const info = state.info || {};
    const metrics = state.metrics || {};
    const host = state.host || {};
    const network = state.network || {};
    const settings = state.settings || {};
    const players = Array.isArray(state.players) ? state.players : [];
    const serverName = info.servername || "Palworld server";
    const online = Boolean(state.online);
    const controlMode = state.controlMode || "docker";
    const controlLabel = controlMode === "windows" ? "Windows" : controlMode === "sample" ? "Sample" : "Docker";

    el("top-server-name").textContent = serverName;
    el("top-server-state").textContent = online ? "Online" : "Offline";
    el("server-description").textContent = info.description || "Private Palworld dedicated server.";
    el("sample-label").hidden = !state.sampleMode;

    const topDot = el("top-status-dot");
    topDot.className = `status-dot ${online ? "is-online" : "is-offline"}`;

    const metricStatus = el("metric-status");
    metricStatus.textContent = online ? "Online" : "Offline";
    metricStatus.className = `metric-value status-value ${online ? "is-online" : "is-offline"}`;
    el("metric-container").textContent = `${controlLabel} ${state.containerStatus || "unknown"}`;
    el("metric-players").textContent = `${metrics.currentplayernum ?? players.length}/${metrics.maxplayernum ?? settings.ServerPlayerMaxNum ?? 4}`;
    el("metric-fps").textContent = online && Number.isFinite(metrics.serverfps) ? formatNumber(metrics.serverfps, 1) : "-";
    el("metric-frame-time").textContent = online && Number.isFinite(metrics.serverframetime)
      ? `${formatNumber(metrics.serverframetime, 2)} ms frame time`
      : "No frame data";
    el("metric-memory").textContent = compactMemory(host.memoryUsage || "-");
    el("metric-memory-percent").textContent = host.memoryPercent ? `${host.memoryPercent} of limit` : "Memory usage";
    el("metric-cpu").textContent = host.cpuPercent || "-";
    const networkStatus = network.status || "collecting";
    const networkValue = el("metric-network");
    networkValue.textContent = network.enabled && Number.isFinite(network.latencyMs) && network.received > 0
      ? `${formatNumber(network.latencyMs, 1)} ms`
      : network.enabled === false
        ? "Disabled"
        : "Checking";
    networkValue.className = `metric-value network-value is-${networkStatus}`;
    el("metric-network-loss").textContent = networkStatus === "collecting"
      ? `Collecting ${network.sent || 0}/${network.windowSize || 0} probes`
      : network.enabled === false
        ? "Network monitor disabled"
        : `${formatNumber(network.packetLoss || 0, 1)}% loss · ${networkStatus}`;
    el("metric-uptime").textContent = online ? formatDuration(metrics.uptime || 0) : "-";
    el("metric-day").textContent = Number.isFinite(metrics.days) ? `World day ${metrics.days}` : "World day unknown";
    el("nav-player-count").textContent = String(players.length);
    el("chart-current").textContent = online && Number.isFinite(metrics.serverfps) ? `${formatNumber(metrics.serverfps, 1)} FPS` : "-";

    el("detail-version").textContent = info.version || "-";
    el("detail-difficulty").textContent = settings.Difficulty || "-";
    el("detail-exp").textContent = rate(settings.ExpRate);
    el("detail-capture").textContent = rate(settings.PalCaptureRate);
    el("detail-death").textContent = settings.DeathPenalty || "-";
    el("detail-crossplay").textContent = crossplay(settings);

    el("connection-rest").textContent = online ? "Connected" : "Unavailable";
    el("connection-docker").textContent = state.containerStatus || "Unknown";
    el("connection-rcon").textContent = state.features?.rcon ? "Configured" : "Not configured";
    el("connection-backup").textContent = state.features?.backup ? "Configured" : "Not configured";
    el("update-button").disabled = !state.features?.update;
    el("backup-button").disabled = !state.features?.backup;

    const issues = Array.isArray(state.issues) ? state.issues : [];
    el("issue-banner").hidden = issues.length === 0;
    el("issue-message").textContent = issues.join(" | ");

    const updated = new Date(state.updatedAt || Date.now());
    el("last-updated").textContent = `Updated ${updated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;

    renderOverviewPlayers(players);
    renderPlayersTable(players);
    all("[data-action='start']").forEach((button) => {
      if (!button.dataset.startLabel) button.dataset.startLabel = button.textContent.trim();
      button.textContent = online ? "Running" : button.dataset.startLabel;
      button.disabled = online;
      button.title = online ? "The server is already running" : "Start the Palworld server";
    });
  }

  function renderOverviewPlayers(players) {
    const list = el("overview-player-list");
    const summary = el("online-summary");
    list.replaceChildren();
    if (!players.length) {
      summary.textContent = "Nobody is connected.";
      list.append(emptyState("No players online", "The server is quiet right now.", true));
      return;
    }
    summary.textContent = `${players.length} ${players.length === 1 ? "friend is" : "friends are"} connected.`;
    players.forEach((player) => {
      const row = document.createElement("div");
      row.className = "player-row";

      const avatar = document.createElement("span");
      avatar.className = "player-avatar";
      avatar.textContent = initials(player.name);

      const identity = document.createElement("div");
      const name = document.createElement("span");
      name.className = "player-name";
      name.textContent = player.name || "Unknown player";
      const meta = document.createElement("span");
      meta.className = "player-meta";
      meta.textContent = `Level ${player.level ?? "-"}`;
      identity.append(name, meta);

      const ping = document.createElement("span");
      ping.className = "ping";
      ping.textContent = Number.isFinite(player.ping) ? `${Math.round(player.ping)} ms` : "-";
      row.append(avatar, identity, ping);
      list.append(row);
    });
  }

  function renderPlayersTable(players) {
    const body = el("players-table-body");
    const empty = el("players-empty");
    body.replaceChildren();
    empty.hidden = players.length > 0;
    el("players-table-summary").textContent = players.length
      ? `${players.length} ${players.length === 1 ? "player" : "players"} connected right now.`
      : "The server currently has no connected players.";

    players.forEach((player) => {
      const row = document.createElement("tr");

      const identityCell = document.createElement("td");
      identityCell.className = "table-player";
      identityCell.textContent = player.name || "Unknown player";
      const account = document.createElement("small");
      account.textContent = player.accountName || player.userId || "Unknown account";
      identityCell.append(account);

      const levelCell = document.createElement("td");
      levelCell.textContent = player.level ?? "-";
      const pingCell = document.createElement("td");
      pingCell.textContent = Number.isFinite(player.ping) ? `${Math.round(player.ping)} ms` : "-";
      const buildingsCell = document.createElement("td");
      buildingsCell.textContent = player.building_count ?? "-";

      const actionsCell = document.createElement("td");
      const actions = document.createElement("div");
      actions.className = "table-actions";
      actions.append(
        playerActionButton("Kick", "kick", player),
        playerActionButton("Ban", "ban", player, true),
      );
      actionsCell.append(actions);
      row.append(identityCell, levelCell, pingCell, buildingsCell, actionsCell);
      body.append(row);
    });
  }

  function playerActionButton(label, action, player, danger = false) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `mini-button${danger ? " is-danger" : ""}`;
    button.textContent = label;
    button.dataset.playerAction = action;
    button.dataset.userId = player.userId || "";
    button.dataset.playerName = player.name || "this player";
    return button;
  }

  async function loadHistory() {
    try {
      const result = await api("/api/history");
      app.history = Array.isArray(result.samples) ? result.samples : [];
      drawChart();
    } catch (error) {
      app.history = [];
      drawChart();
    }
  }

  function drawChart() {
    const canvas = el("performance-chart");
    const wrapper = canvas.parentElement;
    const points = app.history.filter((point) => Number.isFinite(point.fps));
    el("chart-empty").hidden = points.length > 1;
    if (points.length < 2) {
      const context = canvas.getContext("2d");
      context.clearRect(0, 0, canvas.width, canvas.height);
      return;
    }

    const rect = wrapper.getBoundingClientRect();
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.max(1, Math.floor(rect.width * ratio));
    canvas.height = Math.max(1, Math.floor(rect.height * ratio));
    canvas.style.width = `${rect.width}px`;
    canvas.style.height = `${rect.height}px`;

    const context = canvas.getContext("2d");
    context.scale(ratio, ratio);
    const width = rect.width;
    const height = rect.height;
    const padding = { top: 12, right: 10, bottom: 24, left: 34 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    const values = points.map((point) => point.fps);
    const rawMin = Math.min(...values);
    const rawMax = Math.max(...values);
    const min = Math.max(0, Math.floor(rawMin - 2));
    const max = Math.max(min + 5, Math.ceil(rawMax + 2));

    context.clearRect(0, 0, width, height);
    context.font = '10px "Cascadia Code", monospace';
    context.fillStyle = "#788179";
    context.strokeStyle = "#303831";
    context.lineWidth = 1;

    [0, 0.5, 1].forEach((position) => {
      const y = padding.top + chartHeight * position;
      context.beginPath();
      context.moveTo(padding.left, y + 0.5);
      context.lineTo(width - padding.right, y + 0.5);
      context.stroke();
      const label = Math.round(max - (max - min) * position);
      context.fillText(String(label), 2, y + 4);
    });

    const firstTime = new Date(points[0].at);
    const lastTime = new Date(points[points.length - 1].at);
    context.fillText(firstTime.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }), padding.left, height - 5);
    const lastLabel = lastTime.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    const lastWidth = context.measureText(lastLabel).width;
    context.fillText(lastLabel, width - padding.right - lastWidth, height - 5);

    context.beginPath();
    points.forEach((point, index) => {
      const x = padding.left + (index / (points.length - 1)) * chartWidth;
      const normalized = (point.fps - min) / (max - min);
      const y = padding.top + chartHeight - normalized * chartHeight;
      if (index === 0) context.moveTo(x, y);
      else context.lineTo(x, y);
    });
    context.strokeStyle = "#9fdbaf";
    context.lineWidth = 2;
    context.lineJoin = "round";
    context.lineCap = "round";
    context.stroke();
  }

  async function loadLogs({ scroll = true } = {}) {
    if (app.activeView !== "console") return;
    const output = el("console-output");
    try {
      const result = await api("/api/logs");
      const lines = Array.isArray(result.lines) ? result.lines : [];
      output.textContent = lines.length ? lines.join("\n") : "No container logs yet.";
      el("console-status").textContent = `Updated ${new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`;
      if (scroll) output.scrollTop = output.scrollHeight;
    } catch (error) {
      output.textContent = `Could not read logs.\n${error.message}`;
      el("console-status").textContent = "Logs unavailable";
    }
  }

  async function loadBackups() {
    const list = el("backup-list");
    try {
      const result = await api("/api/backups");
      const backups = Array.isArray(result.backups) ? result.backups : [];
      list.replaceChildren();
      if (!backups.length) {
        list.append(emptyState("No backups found", "Create one before the next server update.", true));
        return;
      }
      backups.forEach((backup) => {
        const row = document.createElement("div");
        row.className = "backup-row";
        const info = document.createElement("div");
        const name = document.createElement("strong");
        name.textContent = backup.name;
        const date = document.createElement("span");
        date.textContent = new Date(backup.createdAt).toLocaleString([], {
          dateStyle: "medium",
          timeStyle: "short",
        });
        info.append(name, date);
        const size = document.createElement("span");
        size.className = "backup-size";
        size.textContent = formatBytes(backup.size);
        row.append(info, size);
        list.append(row);
      });
    } catch (error) {
      list.replaceChildren(emptyState("Backups unavailable", error.message, true));
    }
  }

  async function loadGameSettings({ quiet = false } = {}) {
    if (app.settingsLoading) return;
    app.settingsLoading = true;
    if (!quiet) {
      el("settings-source").textContent = "Loading current settings";
      el("settings-form").setAttribute("aria-busy", "true");
    }
    try {
      const result = await api("/api/game-settings");
      app.settingsData = result;
      app.settingsDraft.clear();
      renderGameSettings();
    } catch (error) {
      el("settings-source").textContent = "Settings unavailable";
      el("preset-grid").replaceChildren(emptyState("Could not load presets", error.message, true));
      el("settings-field-list").replaceChildren(emptyState("Could not load settings", error.message, true));
      if (!quiet) toast(error.message, true);
    } finally {
      app.settingsLoading = false;
      el("settings-form").removeAttribute("aria-busy");
    }
  }

  function renderGameSettings() {
    const data = app.settingsData;
    if (!data) return;
    const groups = Array.isArray(data.groups) ? data.groups : [];
    if (!groups.some((group) => group.id === app.settingsGroup)) {
      app.settingsGroup = groups[0]?.id || "";
    }
    el("settings-source").textContent = `${data.definitions?.length || 0} controls · ${data.source || "unknown source"}`;
    el("rollback-settings-button").disabled = !data.editable || !data.rollbackAvailable;
    const notice = el("settings-notice");
    notice.classList.toggle("is-warning", !data.editable);
    notice.querySelector("strong").textContent = data.editable ? "Safe restart workflow" : "Read-only settings";
    notice.querySelector("span").textContent = data.editable
      ? "PAL CTRL saves the world, creates a backup, writes the configuration, restarts Palworld and verifies recovery."
      : "Settings are visible, but PALWORLD_SETTINGS_PATH is not configured for editing.";
    renderPresets();
    renderSettingsCategories();
    renderSettingsFields();
    syncSettingsDraftUI();
  }

  function renderPresets() {
    const grid = el("preset-grid");
    grid.replaceChildren();
    const presets = Array.isArray(app.settingsData?.presets) ? app.settingsData.presets : [];
    presets.forEach((preset) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `preset-card preset-${preset.tone || "normal"}`;
      button.dataset.preset = preset.id;
      button.disabled = !app.settingsData.editable;

      const top = document.createElement("span");
      top.className = "preset-card-top";
      const name = document.createElement("strong");
      name.textContent = preset.name;
      const action = document.createElement("span");
      action.className = "preset-action";
      action.textContent = "Preview";
      top.append(name, action);

      const description = document.createElement("span");
      description.className = "preset-description";
      description.textContent = preset.description;

      const changes = document.createElement("span");
      changes.className = "preset-changes";
      if (preset.baseline) {
        changes.textContent = "Captured baseline";
      } else {
        const entries = Object.entries(preset.changes || {});
        changes.textContent = `${entries.length} ${entries.length === 1 ? "setting" : "settings"}`;
      }
      if (!preset.baseline && presetMatchesValues(preset, app.settingsData.values || {})) {
        button.classList.add("is-active");
        changes.textContent += " · Active";
      }
      button.append(top, description, changes);
      grid.append(button);
    });
  }

  function presetMatchesValues(preset, values) {
    const changes = Object.entries(preset.changes || {});
    return changes.length > 0 && changes.every(([key, value]) => valuesEqual(values[key], value));
  }

  function renderSettingsCategories() {
    const navigation = el("settings-categories");
    navigation.replaceChildren();
    const definitions = Array.isArray(app.settingsData?.definitions) ? app.settingsData.definitions : [];
    (app.settingsData?.groups || []).forEach((group) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `settings-category${group.id === app.settingsGroup ? " is-active" : ""}`;
      button.dataset.settingsGroup = group.id;
      const name = document.createElement("span");
      name.textContent = group.name;
      const count = document.createElement("span");
      count.textContent = String(definitions.filter((definition) => definition.group === group.id).length);
      button.append(name, count);
      navigation.append(button);
    });
  }

  function renderSettingsFields() {
    const list = el("settings-field-list");
    const data = app.settingsData;
    if (!data) return;
    const groups = Array.isArray(data.groups) ? data.groups : [];
    const definitions = Array.isArray(data.definitions) ? data.definitions : [];
    const search = app.settingsSearch.trim().toLowerCase();
    const group = groups.find((item) => item.id === app.settingsGroup);

    el("settings-group-title").textContent = search ? "Search results" : group?.name || "Settings";
    el("settings-group-description").textContent = search
      ? `Official controls matching “${app.settingsSearch.trim()}”.`
      : group?.description || "Choose a category to begin.";

    const filtered = definitions.filter((definition) => {
      if (!search) return definition.group === app.settingsGroup;
      return [definition.label, definition.description, definition.key, definition.group]
        .join(" ")
        .toLowerCase()
        .includes(search);
    });
    list.replaceChildren();
    el("settings-empty").hidden = filtered.length > 0;
    filtered.forEach((definition) => list.append(createSettingField(definition)));
    syncSettingsDraftUI();
  }

  function createSettingField(definition) {
    const wrapper = document.createElement("div");
    wrapper.className = "settings-field";
    wrapper.dataset.settingRow = definition.key;
    wrapper.classList.toggle("is-changed", app.settingsDraft.has(definition.key));

    const copy = document.createElement("div");
    copy.className = "settings-field-copy";
    const label = document.createElement("label");
    label.htmlFor = `setting-${definition.key}`;
    label.textContent = definition.label;
    const description = document.createElement("span");
    description.textContent = definition.description;
    copy.append(label, description);
    if (definition.warning) {
      const warning = document.createElement("small");
      warning.textContent = definition.warning;
      copy.append(warning);
    }

    const control = document.createElement("div");
    control.className = "settings-control";
    const original = app.settingsData.values?.[definition.key] ?? definition.default;
    const value = app.settingsDraft.has(definition.key) ? app.settingsDraft.get(definition.key) : original;
    let input;

    if (definition.type === "boolean") {
      const switchLabel = document.createElement("label");
      switchLabel.className = "toggle-control";
      input = document.createElement("input");
      input.type = "checkbox";
      input.checked = Boolean(value);
      input.id = `setting-${definition.key}`;
      input.dataset.settingKey = definition.key;
      const track = document.createElement("span");
      track.className = "toggle-track";
      const state = document.createElement("span");
      state.className = "toggle-state";
      state.textContent = input.checked ? "Enabled" : "Disabled";
      switchLabel.append(input, track, state);
      control.append(switchLabel);
    } else if (definition.type === "select") {
      input = document.createElement("select");
      input.id = `setting-${definition.key}`;
      input.dataset.settingKey = definition.key;
      (definition.options || []).forEach((option) => {
        const element = document.createElement("option");
        element.value = option.value;
        element.textContent = option.label;
        input.append(element);
      });
      input.value = String(value ?? "");
      control.append(input);
    } else if (definition.type === "list") {
      input = document.createElement("textarea");
      input.id = `setting-${definition.key}`;
      input.dataset.settingKey = definition.key;
      input.rows = 2;
      input.placeholder = "Technology IDs separated by commas";
      input.value = Array.isArray(value) ? value.join(", ") : "";
      control.classList.add("is-wide-control");
      control.append(input);
    } else {
      input = document.createElement("input");
      input.id = `setting-${definition.key}`;
      input.dataset.settingKey = definition.key;
      input.type = definition.type === "text" ? "text" : "number";
      input.value = value ?? "";
      if (definition.min !== undefined) input.min = String(definition.min);
      if (definition.max !== undefined) input.max = String(definition.max);
      if (definition.step !== undefined) input.step = String(definition.step);
      control.append(input);
      if (definition.unit) {
        const unit = document.createElement("span");
        unit.className = "setting-unit";
        unit.textContent = definition.unit;
        control.append(unit);
      }
    }
    input.disabled = !app.settingsData.editable;
    wrapper.append(copy, control);
    return wrapper;
  }

  function handleSettingInput(input) {
    const definition = app.settingsData?.definitions?.find((item) => item.key === input.dataset.settingKey);
    if (!definition) return;
    let value;
    if (definition.type === "boolean") {
      value = input.checked;
      const state = input.closest(".toggle-control")?.querySelector(".toggle-state");
      if (state) state.textContent = input.checked ? "Enabled" : "Disabled";
    } else if (definition.type === "number" || definition.type === "integer") {
      if (input.value === "" || !input.validity.valid) return;
      value = Number(input.value);
    } else if (definition.type === "list") {
      value = input.value.split(",").map((item) => item.trim()).filter(Boolean);
    } else {
      value = input.value;
    }
    const original = app.settingsData.values?.[definition.key] ?? definition.default;
    if (valuesEqual(value, original)) app.settingsDraft.delete(definition.key);
    else app.settingsDraft.set(definition.key, value);
    input.closest(".settings-field")?.classList.toggle("is-changed", app.settingsDraft.has(definition.key));
    syncSettingsDraftUI();
  }

  function syncSettingsDraftUI() {
    const count = app.settingsDraft.size;
    const editable = Boolean(app.settingsData?.editable);
    el("apply-settings-button").disabled = !editable || count === 0;
    el("discard-settings-button").disabled = count === 0;
    el("pending-change-count").textContent = count
      ? `${count} pending ${count === 1 ? "change" : "changes"}`
      : "No pending changes";
    const navCount = el("nav-settings-count");
    navCount.hidden = count === 0;
    navCount.textContent = String(count);
    el("settings-summary").textContent = `${app.settingsData?.definitions?.length || 0} official controls. Changes apply after a safe restart.`;
  }

  function valuesEqual(left, right) {
    if (Array.isArray(left) || Array.isArray(right)) {
      if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false;
      return left.every((value, index) => String(value) === String(right[index]));
    }
    if (typeof left === "number" || typeof right === "number") return Number(left) === Number(right);
    return left === right;
  }

  function settingDisplayValue(definition, value) {
    if (definition?.type === "boolean") return value ? "Enabled" : "Disabled";
    if (definition?.type === "list") return Array.isArray(value) && value.length ? value.join(", ") : "None";
    if (definition?.type === "select") {
      return definition.options?.find((option) => option.value === value)?.label || String(value);
    }
    if (definition?.unit) return `${value} ${definition.unit}`;
    return String(value);
  }

  function openSettingsConfirmation({ title, message, preset = "", changes = null, rollback = false }) {
    const preview = el("settings-change-preview");
    preview.replaceChildren();
    if (rollback) {
      preview.append(createSettingsPreviewRow("Configuration", "Current", "Previous snapshot"));
    } else if (preset === "normal") {
      preview.append(createSettingsPreviewRow("Profile", "Current settings", "Captured baseline"));
    } else {
      const values = changes || {};
      Object.entries(values).forEach(([key, nextValue]) => {
        const definition = app.settingsData?.definitions?.find((item) => item.key === key);
        if (!definition) return;
        const currentValue = app.settingsData.values?.[key] ?? definition.default;
        preview.append(createSettingsPreviewRow(
          definition.label,
          settingDisplayValue(definition, currentValue),
          settingDisplayValue(definition, nextValue),
        ));
      });
    }
    el("settings-confirm-title").textContent = title;
    el("settings-confirm-message").textContent = message;
    app.pendingSettingsAction = rollback
      ? { type: "rollback" }
      : { type: "apply", payload: preset ? { preset } : { changes } };
    el("settings-confirm-dialog").showModal();
  }

  function createSettingsPreviewRow(label, before, after) {
    const row = document.createElement("div");
    row.className = "settings-preview-row";
    const name = document.createElement("strong");
    name.textContent = label;
    const values = document.createElement("span");
    const oldValue = document.createElement("s");
    oldValue.textContent = before;
    const arrow = document.createElement("i");
    arrow.textContent = "to";
    const newValue = document.createElement("b");
    newValue.textContent = after;
    values.append(oldValue, arrow, newValue);
    row.append(name, values);
    return row;
  }

  async function runSettingsAction(action) {
    const applyButton = el("apply-settings-button");
    const rollbackButton = el("rollback-settings-button");
    const submitButton = el("settings-confirm-submit");
    [applyButton, rollbackButton, submitButton].forEach((button) => {
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
    });
    el("settings-notice").classList.add("is-working");
    el("settings-notice").querySelector("strong").textContent = "Safe restart in progress";
    el("settings-notice").querySelector("span").textContent = "Saving, backing up and restarting Palworld. Keep this page open.";
    try {
      const path = action.type === "rollback" ? "/api/game-settings/rollback" : "/api/game-settings/apply";
      const body = action.type === "rollback" ? "{}" : JSON.stringify(action.payload);
      const result = await api(path, { method: "POST", body });
      toast(result.message || "Settings applied");
      app.settingsData = null;
      app.settingsDraft.clear();
      await loadGameSettings();
      await loadState({ quiet: true });
      await loadBackups();
    } catch (error) {
      toast(error.message, true);
      if (app.settingsData) renderGameSettings();
    } finally {
      [applyButton, rollbackButton, submitButton].forEach((button) => {
        button.disabled = false;
        button.removeAttribute("aria-busy");
      });
      el("settings-notice").classList.remove("is-working");
      if (app.settingsData) renderGameSettings();
    }
  }

  function switchView(view) {
    if (!["overview", "players", "console", "utilities", "settings"].includes(view)) return;
    app.activeView = view;
    all("[data-view-panel]").forEach((panel) => {
      const visible = panel.dataset.viewPanel === view;
      panel.hidden = !visible;
      panel.classList.toggle("is-visible", visible);
    });
    all("[data-view]").forEach((button) => {
      button.classList.toggle("is-active", button.dataset.view === view);
    });
    history.replaceState(null, "", `#${view}`);
    el("main-content").focus({ preventScroll: true });
    if (view === "console") {
      loadLogs();
      window.clearInterval(app.logTimer);
      app.logTimer = window.setInterval(() => loadLogs({ scroll: false }), 5000);
    } else {
      window.clearInterval(app.logTimer);
    }
    if (view === "utilities") loadBackups();
    if (view === "settings" && !app.settingsData) loadGameSettings();
    if (view === "overview") drawChart();
  }

  async function performAction(request, trigger) {
    const button = trigger || null;
    if (button) {
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
    }
    try {
      const result = await api("/api/action", {
        method: "POST",
        body: JSON.stringify(request),
      });
      toast(result.message || "Action completed");
      if (request.action === "backup") await loadBackups();
      if (["start", "restart", "shutdown", "force-stop"].includes(request.action)) {
        await loadState({ quiet: true });
      } else {
        window.setTimeout(() => loadState({ quiet: true }), 1200);
      }
    } catch (error) {
      toast(error.message, true);
    } finally {
      if (button) {
        button.disabled = false;
        button.removeAttribute("aria-busy");
      }
    }
  }

  function requestAction(action, button) {
    const confirmations = {
      restart: ["Restart the server?", "Connected players will be disconnected. Save the world first if needed."],
      shutdown: ["Shut down gracefully?", "Players will receive a warning before the server stops."],
      update: ["Update the server?", "The configured update workflow will validate and refresh the server files."],
      "force-stop": ["Force stop the server?", "This skips the in-game save and shutdown path. Unsaved progress may be lost."],
    };
    if (confirmations[action]) {
      confirmAction(confirmations[action][0], confirmations[action][1], () => {
        performAction({ action }, button);
      }, action === "force-stop");
      return;
    }
    performAction({ action }, button);
  }

  function confirmAction(title, message, onConfirm, danger = false) {
    const dialog = el("confirm-dialog");
    el("confirm-title").textContent = title;
    el("confirm-message").textContent = message;
    const submit = el("confirm-submit");
    submit.textContent = danger ? "Force stop" : "Continue";
    submit.className = `button ${danger ? "button-danger" : "button-primary"}`;
    app.pendingAction = onConfirm;
    dialog.showModal();
  }

  function toast(message, error = false) {
    const region = el("toast-region");
    const item = document.createElement("div");
    item.className = `toast${error ? " is-error" : ""}`;
    item.textContent = message;
    region.append(item);
    window.setTimeout(() => item.remove(), 4500);
  }

  function emptyState(title, detail, compact = false) {
    const wrapper = document.createElement("div");
    wrapper.className = `empty-state${compact ? " compact" : ""}`;
    const strong = document.createElement("strong");
    strong.textContent = title;
    const span = document.createElement("span");
    span.textContent = detail;
    wrapper.append(strong, span);
    return wrapper;
  }

  function initials(name = "") {
    return name
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .map((part) => part[0] || "")
      .join("")
      .toUpperCase() || "?";
  }

  function formatNumber(value, digits = 0) {
    return Number(value).toLocaleString(undefined, {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    });
  }

  function formatDuration(seconds) {
    const value = Math.max(0, Number(seconds) || 0);
    const days = Math.floor(value / 86400);
    const hours = Math.floor((value % 86400) / 3600);
    const minutes = Math.floor((value % 3600) / 60);
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  }

  function formatBytes(bytes) {
    const value = Number(bytes) || 0;
    if (value < 1024) return `${value} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let size = value / 1024;
    let index = 0;
    while (size >= 1024 && index < units.length - 1) {
      size /= 1024;
      index += 1;
    }
    return `${size.toFixed(size >= 100 ? 0 : 1)} ${units[index]}`;
  }

  function compactMemory(value) {
    if (!value || value === "-") return "-";
    return value.split("/")[0].trim();
  }

  function rate(value) {
    return Number.isFinite(value) ? `${formatNumber(value, value % 1 ? 1 : 0)}x` : "-";
  }

  function crossplay(settings) {
    const value = settings.CrossplayPlatforms || settings.AllowConnectPlatform;
    if (Array.isArray(value)) return value.join(", ");
    if (typeof value === "string" && value.trim()) return value.replace(/[()]/g, "");
    return "-";
  }

  function bindEvents() {
    all("[data-view]").forEach((button) => {
      button.addEventListener("click", () => switchView(button.dataset.view));
    });
    all("[data-view-jump]").forEach((button) => {
      button.addEventListener("click", () => switchView(button.dataset.viewJump));
    });
    all("[data-view-link]").forEach((link) => {
      link.addEventListener("click", (event) => {
        event.preventDefault();
        switchView(link.dataset.viewLink);
      });
    });
    all("[data-action]").forEach((button) => {
      button.addEventListener("click", () => requestAction(button.dataset.action, button));
    });

    el("refresh-button").addEventListener("click", () => loadState());
    el("logout-button").addEventListener("click", async () => {
      try {
        await api("/api/logout", { method: "POST", body: "{}" });
      } catch {
        // The lock screen is still safe to show if the request fails.
      }
      showLogin();
    });

    el("login-form").addEventListener("submit", async (event) => {
      event.preventDefault();
      const button = event.submitter;
      const password = el("panel-password").value;
      button.disabled = true;
      el("login-error").textContent = "";
      try {
        await api("/api/login", { method: "POST", body: JSON.stringify({ password }) });
        el("panel-password").value = "";
        hideLogin();
        await loadState();
      } catch (error) {
        el("login-error").textContent = error.message;
        showLogin();
      } finally {
        button.disabled = false;
      }
    });

    el("announce-form").addEventListener("submit", async (event) => {
      event.preventDefault();
      const input = el("announce-message");
      const message = input.value.trim();
      if (!message) {
        toast("Write an announcement first", true);
        input.focus();
        return;
      }
      await performAction({ action: "announce", message }, event.submitter);
      input.value = "";
    });

    el("players-table-body").addEventListener("click", (event) => {
      const button = event.target.closest("[data-player-action]");
      if (!button) return;
      const action = button.dataset.playerAction;
      const playerName = button.dataset.playerName;
      confirmAction(
        `${action === "ban" ? "Ban" : "Kick"} ${playerName}?`,
        action === "ban"
          ? "This player will be removed and prevented from rejoining."
          : "This player will be removed from the current session.",
        () => performAction({ action, userId: button.dataset.userId }, button),
        action === "ban",
      );
    });

    el("console-form").addEventListener("submit", async (event) => {
      event.preventDefault();
      const input = el("console-command");
      const command = input.value.trim();
      if (!command) return;
      const output = el("console-output");
      output.textContent += `\n> ${command}\n`;
      event.submitter.disabled = true;
      try {
        const result = await api("/api/console", {
          method: "POST",
          body: JSON.stringify({ command }),
        });
        output.textContent += `${result.output || "Command completed"}\n`;
        output.scrollTop = output.scrollHeight;
        input.value = "";
      } catch (error) {
        output.textContent += `Error: ${error.message}\n`;
        output.scrollTop = output.scrollHeight;
      } finally {
        event.submitter.disabled = false;
        input.focus();
      }
    });

    el("clear-console").addEventListener("click", () => {
      el("console-output").textContent = "Console view cleared. New logs will appear on the next refresh.";
    });

    el("preset-grid").addEventListener("click", (event) => {
      const button = event.target.closest("[data-preset]");
      if (!button || button.disabled) return;
      const preset = app.settingsData?.presets?.find((item) => item.id === button.dataset.preset);
      if (!preset) return;
      openSettingsConfirmation({
        title: `Apply ${preset.name}?`,
        message: "The world will be saved and backed up. Connected players will need to reconnect after the restart.",
        preset: preset.id,
        changes: preset.changes || {},
      });
    });

    el("settings-categories").addEventListener("click", (event) => {
      const button = event.target.closest("[data-settings-group]");
      if (!button) return;
      app.settingsGroup = button.dataset.settingsGroup;
      app.settingsSearch = "";
      el("settings-search").value = "";
      renderSettingsCategories();
      renderSettingsFields();
    });

    el("settings-form").addEventListener("input", (event) => {
      const input = event.target.closest("[data-setting-key]");
      if (input) handleSettingInput(input);
    });
    el("settings-form").addEventListener("change", (event) => {
      const input = event.target.closest("[data-setting-key]");
      if (input) handleSettingInput(input);
    });

    el("settings-search").addEventListener("input", (event) => {
      app.settingsSearch = event.target.value;
      renderSettingsFields();
    });

    el("discard-settings-button").addEventListener("click", () => {
      app.settingsDraft.clear();
      renderSettingsFields();
      toast("Pending settings discarded");
    });

    el("apply-settings-button").addEventListener("click", () => {
      if (!app.settingsDraft.size) return;
      const changes = Object.fromEntries(app.settingsDraft);
      openSettingsConfirmation({
        title: `Apply ${app.settingsDraft.size} ${app.settingsDraft.size === 1 ? "change" : "changes"}?`,
        message: "The world will be saved and backed up. Connected players will need to reconnect after the restart.",
        changes,
      });
    });

    el("rollback-settings-button").addEventListener("click", () => {
      openSettingsConfirmation({
        title: "Undo the last settings change?",
        message: "The previous configuration snapshot will be restored through the same safe restart workflow.",
        rollback: true,
      });
    });

    el("confirm-dialog").addEventListener("close", () => {
      if (el("confirm-dialog").returnValue === "confirm" && app.pendingAction) {
        const callback = app.pendingAction;
        app.pendingAction = null;
        callback();
      } else {
        app.pendingAction = null;
      }
    });

    el("settings-confirm-dialog").addEventListener("close", () => {
      if (el("settings-confirm-dialog").returnValue === "confirm" && app.pendingSettingsAction) {
        const action = app.pendingSettingsAction;
        app.pendingSettingsAction = null;
        runSettingsAction(action);
      } else {
        app.pendingSettingsAction = null;
      }
    });

    app.chartResizeObserver = new ResizeObserver(() => {
      if (app.activeView === "overview") drawChart();
    });
    app.chartResizeObserver.observe(el("performance-chart").parentElement);
  }

  function start() {
    bindEvents();
    const initialView = window.location.hash.slice(1);
    switchView(["overview", "players", "console", "utilities", "settings"].includes(initialView) ? initialView : "overview");
    loadState();
    app.refreshTimer = window.setInterval(() => loadState({ quiet: true }), 10000);
  }

  document.addEventListener("DOMContentLoaded", start, { once: true });
})();
