// Completist, the ROM library manager that tells you what's missing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/backup"
	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/covers"
	"github.com/DeLFuS77/completist/internal/datsync"
	"github.com/DeLFuS77/completist/internal/notify"
	"github.com/DeLFuS77/completist/internal/scanner"
	"github.com/DeLFuS77/completist/internal/server"
	"github.com/DeLFuS77/completist/internal/stats"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/tasks"
	"github.com/DeLFuS77/completist/internal/watcher"
	"github.com/DeLFuS77/completist/internal/webui"
)

func env(name string, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(env(name, "")); err == nil {
		return v
	}
	return fallback
}

func main() {
	defaultData := "data"
	if exe, err := os.Executable(); err == nil {
		defaultData = filepath.Join(filepath.Dir(exe), "data")
	}
	dataDir := flag.String("data", env("COMPLETIST_DATA_DIR", defaultData), "folder for the database and settings")
	port := flag.Int("port", envInt("COMPLETIST_PORT", 3000), "port of the web interface")
	host := flag.String("host", env("COMPLETIST_HOST", ""), "address to listen on (all by default)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("completist", server.Version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log, *dataDir, net.JoinHostPort(*host, strconv.Itoa(*port))); err != nil {
		log.Error("completist stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, dataDir string, address string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("data folder %s: %w", dataDir, err)
	}
	probe := filepath.Join(dataDir, ".write-test")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("the data folder %s is not writable: %w (with Docker, check PUID and PGID)", dataDir, err)
	}
	os.Remove(probe)

	st, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	authn, err := auth.New(dataDir)
	if err != nil {
		return err
	}
	if weak := authn.WeakEnvPassword(); weak != nil {
		log.Warn("the password in COMPLETIST_AUTH_PASSWORD is weak", "reason", weak.Error())
	}

	taskLog := tasks.Load(dataDir)
	activityLog := activity.Load(dataDir)
	authn.OnAction = activityLog.Add
	progress := collection.NewCache(st)
	notifier := &notify.Notifier{Store: st, Progress: progress, DataFolder: dataDir, Log: log}
	sc := scanner.New(st, taskLog, envInt("COMPLETIST_SCAN_WORKERS", 0))
	// today's count, for the history of the statistics
	recordHistory := func() {
		if err := stats.Record(context.Background(), st, progress, time.Now()); err != nil {
			log.Warn("the history could not be saved", "error", err)
		}
	}
	sc.OnChange = func() {
		progress.Invalidate()
		notifier.AfterScan(context.Background())
		recordHistory()
	}
	backups := &backup.Manager{DataFolder: dataDir, Store: st, Auth: authn, Activity: activityLog}
	backups.OnRestore = func() {
		progress.Invalidate()
		notifier.Record(context.Background())
		recordHistory()
		sc.Start(tasks.TriggerSettings)
	}

	addLibrariesFromEnv(log, st)

	srv := &server.Server{Store: st, Tasks: taskLog, Scanner: sc, Progress: progress, Auth: authn, Log: log,
		Activity: activityLog, Backups: backups, Notifier: notifier,
		Covers: &covers.Cache{DataFolder: dataDir, BaseURL: os.Getenv("COMPLETIST_COVERS_URL"), Log: log},
		UI:     webui.FS(), UIScriptHashes: webui.ScriptHashes()}
	// while developing the interface, serve it from its build folder instead of the binary
	if dir := os.Getenv("COMPLETIST_UI_DIR"); dir != "" {
		srv.UI = http.Dir(dir)
		log.Info("serving the interface from a folder", "folder", dir)
	}
	httpServer := &http.Server{Addr: address, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// a scan running when the app stops ends before the database closes
	sc.Context = ctx
	if env("COMPLETIST_WATCH", "true") != "false" {
		watcher.Start(ctx, watcher.Config{
			DataFolder: dataDir,
			Folders:    func() []string { return libraryPaths(st) },
			Scan: func() bool {
				err := sc.Start(tasks.TriggerWatcher)
				return err == nil || errors.Is(err, scanner.ErrBusy)
			},
			PollInterval: time.Duration(envInt("COMPLETIST_WATCH_INTERVAL_MINUTES", 2)) * time.Minute,
			Log:          log,
		})
	}
	go schedule(ctx, log, st, taskLog, sc, progress, notifier, backups, recordHistory)

	errs := make(chan error, 1)
	go func() {
		log.Info("Completist is running", "address", address, "version", server.Version, "data", dataDir)
		errs <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("stopping")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}
	return nil
}

// libraryPaths are the folders of the libraries, for the watcher.
func libraryPaths(st *store.Store) []string {
	libs, err := st.Libraries()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(libs))
	for _, l := range libs {
		out = append(out, l.Path)
	}
	return out
}

// addLibrariesFromEnv adds the folders of COMPLETIST_LIBRARIES (separated by commas) the first
// time; with Docker, /roms is added when nothing is set and no library exists yet.
func addLibrariesFromEnv(log *slog.Logger, st *store.Store) {
	libs, err := st.Libraries()
	if err != nil || len(libs) > 0 {
		return
	}
	paths := []string{}
	for _, p := range strings.Split(os.Getenv("COMPLETIST_LIBRARIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 && os.Getenv("COMPLETIST_IN_DOCKER") != "" {
		paths = []string{"/roms"}
	}
	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			continue
		}
		if _, err := st.AddLibrary(p, "", false); err == nil {
			log.Info("library added", "path", p)
		}
	}
}

// schedule scans at start, updates the DATs now and then and saves a copy of the state every
// week. The watcher scans when files change; the scheduled scan is a safety net. Unchanged
// files are not read again, so a scan of a library already known is quick.
func schedule(ctx context.Context, log *slog.Logger, st *store.Store, taskLog *tasks.Log, sc *scanner.Scanner, progress *collection.Cache, notifier *notify.Notifier, backups *backup.Manager, recordHistory func()) {
	scanEvery := time.Duration(envInt("COMPLETIST_SCAN_INTERVAL_MINUTES", 360)) * time.Minute
	datEvery := time.Duration(envInt("COMPLETIST_DAT_UPDATE_HOURS", 24*7)) * time.Hour

	sc.Start(tasks.TriggerStartup)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastScan := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if scanEvery > 0 && now.Sub(lastScan) >= scanEvery {
				lastScan = now
				if err := sc.Start(tasks.TriggerSchedule); err == nil {
					log.Info("scheduled scan started")
				}
			}
			if datEvery > 0 && !taskLog.Running(tasks.KindDatSync) {
				// the downloaded DATs imported longest ago, so restarts do not delay updates
				dats, err := st.Dats()
				if err != nil {
					continue
				}
				ids := []string{}
				for _, d := range dats {
					if d.Source == "libretro-database" && now.Sub(d.Imported) >= datEvery {
						ids = append(ids, d.System)
					}
				}
				if len(ids) > 0 {
					datsync.Start(ctx, st, taskLog, tasks.TriggerSchedule, ids, progress.Invalidate, func(changes []store.DatChange) {
						progress.Invalidate()
						notifier.AfterDatUpdate(ctx, changes)
						recordHistory()
					})
				}
			}
			if env("COMPLETIST_AUTO_BACKUP", "true") != "false" && backups.Due(now) {
				if info, err := backups.Make(ctx, now); err != nil {
					log.Warn("the weekly backup could not be made", "error", err)
				} else {
					log.Info("weekly backup saved", "name", info.Name)
				}
			}
		}
	}
}
