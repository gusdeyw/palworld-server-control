# Operations and recovery

## Daily use

### Start

Open Overview or Utilities and select **Start server**. PAL CTRL starts the
configured container or Windows process and waits for the Palworld REST API to
become ready.

### Save

Use **Save world** before planned maintenance. Palworld also performs automatic
saves when configured through `AutoSaveSpan`.

### Graceful shutdown

Use **Shut down** instead of Force Stop. The graceful path warns players, asks
Palworld to save, waits for the configured delay and then stops the process.

### Restart

Restart disconnects players. Save first unless the action performing the
restart already includes the safe workflow.

### Force stop

Use Force Stop only if the REST shutdown path has failed. Unsaved progress can
be lost.

## Game Night presets

### Server identity and capacity

Open **Game Night → Server** to change the name shown by Palworld and the
maximum number of simultaneous players. The player limit accepts Palworld's
supported range of 1–32. Both settings use the same safe save, backup, restart
and automatic-recovery workflow as gameplay changes.

| Preset | Purpose |
| --- | --- |
| Normal Night | Restores the baseline captured before the first preset |
| Fast XP Night | Faster experience and easier capture |
| Breeding Night | Short incubation, faster work and easier Pal upkeep |
| Resource Farming | Higher resource and enemy drops with faster respawns |
| Boss Assist | Strong player advantage while preserving some challenge |
| Boss Delete | Emergency cheat-like advantage for a blocked fight |

Every preset shows a before-and-after preview.

Applying any preset:

1. Announces the restart
2. Saves the world
3. Creates a ZIP backup
4. Snapshots the current INI
5. Writes validated changes
6. Restarts Palworld
7. Waits for the REST health check

If startup fails, the previous INI is restored automatically.

### Return to normal

Normal Night restores the effective settings captured before the first preset.
It does not restore passwords or network fields because those fields are not
part of the editable catalog.

### Undo

**Undo last change** restores the most recent INI snapshot through the same
save, backup, restart and health-check process. The button is disabled until a
settings snapshot exists.

## Advanced settings

Use search or select one of the nine categories. Edited rows receive a visible
changed state, and the navigation shows the number of pending changes.

Select **Discard edits** to reset the browser draft. Select **Apply changes** to
review and restart.

High Pal spawn counts, base limits, worker limits and replication settings can
increase CPU and memory usage. Change performance-sensitive values gradually.

## Backups

PAL CTRL ZIP backups contain the configured Saved directory. They are written
outside the live save path.

Recommended schedule:

- Before server updates
- Before changing mods
- Before Game Night settings
- Before a world reset
- At least daily while the group is active

Periodically copy important backups away from the VPS. Backups stored only on
the same disk do not protect against disk failure or provider loss.

The Backups panel shows the combined ZIP storage usage. Select **Remove** beside
an obsolete archive and confirm the permanent deletion to reclaim disk space.
PAL CTRL only permits removal of regular `.zip` files inside its configured
backup directory; report CSVs, settings history and deployment archives are
outside this action.

## Daily measurement reports

Open **Reports** to review the selected day's availability, latency, maximum
packet loss, FPS and peak player count. The hourly table helps narrow an
incident to a time window. **Download CSV** saves the original measurements for
spreadsheets, provider support tickets or long-term storage.

The report archive is independent from the in-memory Overview chart and
survives control-app restarts. By default, PAL CTRL keeps 30 calendar days
under the persistent backup volume and removes only expired report CSV files.
Copy any report you need to retain longer before its retention window expires.

## Fresh-world reset

A safe reset preserves configuration and moves, rather than immediately
deletes, the existing world.

1. Confirm that no players are online.
2. Save and create a ZIP backup.
3. Stop Palworld.
4. Verify the container or process is stopped.
5. Move the `Saved/SaveGames` directory into a timestamped reset archive.
6. Recreate an empty `SaveGames` directory with the original owner and mode.
7. Leave Palworld stopped until the group is ready.

The next start generates a fresh world and player state. Preserve
`Saved/Config` so passwords, REST settings and Game Night configuration remain
unchanged.

Do not reset by deleting the entire `Saved` directory. That also removes
configuration and can change how the server is reached.

## Restoring a reset world

1. Stop Palworld.
2. Move the current fresh `SaveGames` directory aside.
3. Restore the archived `SaveGames` directory to its original location.
4. Verify ownership and permissions.
5. Start Palworld and check the player list and world state.

Keep both the ZIP backup and moved directory until the restored world has been
verified.

## Restoring a ZIP backup

1. Stop Palworld.
2. Create a safety copy of the current `Saved` directory.
3. Extract the selected archive into a temporary directory.
4. Verify the archive contains the expected Palworld path layout.
5. Replace only the intended save data.
6. Restore correct ownership.
7. Start Palworld and verify REST health, players and world progress.

Never extract an untrusted archive directly as root.

## Update workflow

The panel Update action runs the configured Compose pull and recreate flow.
Before an update:

- Save the world
- Create a backup
- Verify free disk space
- Warn connected players

After an update:

- Confirm both containers are running
- Check the Palworld version
- Confirm REST metrics
- Join the game once
- Retain the pre-update backup

## Health thresholds

For a small four-player server:

- Brief CPU spikes during joins, saves and startup are normal.
- Sustained CPU near saturation can cause low server FPS and rubber-banding.
- Memory usage normally grows with world complexity and uptime, not linearly
  with player count.
- If the container reaches a hard memory limit, Palworld can exit instead of
  merely slowing down.

Use server FPS and actual gameplay behavior together with CPU and memory
metrics.
