package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/swax/mudpot/adventures"
	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/dashboard"
	"github.com/swax/mudpot/internal/eventlog"
	"github.com/swax/mudpot/internal/geoip"
	"github.com/swax/mudpot/internal/history"
	"github.com/swax/mudpot/internal/honeypot"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mudpot:", err)
		os.Exit(1)
	}
}
func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}
func addresses(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var result []string
	for _, address := range strings.Split(value, ",") {
		address = strings.TrimSpace(address)
		if address == "" {
			return nil, errors.New("empty listen address")
		}
		if _, err := strconv.Atoi(address); err == nil {
			address = ":" + address
		}
		result = append(result, address)
	}
	return result, nil
}
func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(out, "MudPot v2 — configurable adventure honeypot\n\nCommands:\n  serve       Run public honeypot listeners\n  dashboard   Run the private read-only monitor\n  init        Copy the starter adventure to a new directory\n  validate    Check adventure configuration\n\nUse mudpot <command> -h for flags. Rewards and root access are simulated.")
		return nil
	}
	command := args[0]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(out)
	logPath := env("MUDPOT_LOG", "data/events.jsonl")
	switch command {
	case "init":
		dir := f.String("dir", "my-adventure", "new adventure directory (must not exist)")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		if err := adventures.Init(*dir); err != nil {
			return err
		}
		fmt.Fprintln(out, "Created editable adventure in", *dir)
		return nil
	case "validate":
		dir := f.String("adventure", env("MUDPOT_ADVENTURE", ""), "adventure directory (empty uses embedded starter)")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		w, err := adventure.Load(*dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Valid: %s (%d rooms, %d puzzles, %d rewards)\n", w.Title, len(w.Rooms), len(w.Puzzles), len(w.Rewards))
		return nil
	case "dashboard":
		address := f.String("listen", env("MUDPOT_DASHBOARD_ADDR", "127.0.0.1:8080"), "loopback IP and port")
		path := f.String("log", logPath, "event log path (locates the default history database)")
		historyPath := f.String("history", env("MUDPOT_HISTORY", ""), "persistent session database (default: log path + .sqlite)")
		geoIPDir := f.String("geoip-dir", env("MUDPOT_GEOIP_DIR", "data/geoip"), "local Country and ASN MMDB directory (empty disables)")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		fmt.Fprintf(out, "Private dashboard: http://%s (loopback only)\n", *address)
		resolver, err := geoip.Open(*geoIPDir)
		if err != nil {
			return fmt.Errorf("GeoIP databases: %w", err)
		}
		defer resolver.Close()
		if resolver != nil {
			fmt.Fprintln(out, "Offline Country/ASN lookups enabled from", *geoIPDir)
		}
		return dashboard.Serve(ctx, dashboard.Config{Address: *address, LogPath: *path, HistoryPath: *historyPath, Token: env("MUDPOT_DASHBOARD_TOKEN", ""), GeoIP: resolver})
	case "serve":
		cfg := honeypot.Defaults()
		dir := f.String("adventure", env("MUDPOT_ADVENTURE", ""), "adventure directory (empty uses embedded starter)")
		tcp := f.String("tcp", env("MUDPOT_PORT", ":2222"), "comma-separated TCP addresses or ports; empty disables")
		ssh := f.String("ssh", env("MUDPOT_SSH_PORT", ":2223"), "comma-separated SSH addresses or ports; empty disables")
		tls := f.String("tls", env("MUDPOT_TLS_PORT", ""), "comma-separated TLS addresses or ports; empty disables")
		f.StringVar(&cfg.StateDir, "state", env("MUDPOT_STATE", "data/state"), "private host-key and TLS directory")
		path := f.String("log", logPath, "JSONL event log (10 MiB, one rotated backup)")
		historyPath := f.String("history", env("MUDPOT_HISTORY", ""), "persistent session database (default: log path + .sqlite)")
		f.IntVar(&cfg.MaxConnections, "max-connections", cfg.MaxConnections, "maximum connections, including handshakes")
		f.IntVar(&cfg.PerIP, "per-ip", cfg.PerIP, "maximum concurrent connections per IP")
		f.IntVar(&cfg.CommandsPerMinute, "commands-per-minute", cfg.CommandsPerMinute, "per-session command budget per minute")
		f.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", cfg.HandshakeTimeout, "maximum SSH/TLS/probe handshake time")
		f.DurationVar(&cfg.IdleTimeout, "idle-timeout", cfg.IdleTimeout, "maximum read idle time")
		f.DurationVar(&cfg.MaxLifetime, "max-lifetime", cfg.MaxLifetime, "absolute connection lifetime")
		f.BoolVar(&cfg.CapturePasswords, "capture-passwords", false, "record attempted passwords in private logs (disabled by default)")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		var err error
		if cfg.TCP, err = addresses(*tcp); err != nil {
			return err
		}
		if cfg.SSH, err = addresses(*ssh); err != nil {
			return err
		}
		if cfg.TLS, err = addresses(*tls); err != nil {
			return err
		}
		w, err := adventure.Load(*dir)
		if err != nil {
			return err
		}
		if err = cfg.Validate(); err != nil {
			return err
		}
		if *historyPath == "" {
			*historyPath = history.Path(*path)
		}
		store, err := history.Open(*historyPath)
		if err != nil {
			return fmt.Errorf("session history: %w", err)
		}
		defer store.Close()
		invalid, err := store.Import(ctx, *path)
		if err != nil {
			return fmt.Errorf("import session history: %w", err)
		}
		if invalid > 0 {
			fmt.Fprintf(out, "History import skipped %d incomplete or invalid log records.\n", invalid)
		}
		logger, err := eventlog.OpenRecorded(*path, 10*1024*1024, store)
		if err != nil {
			return err
		}
		defer logger.Close()
		server, err := honeypot.New(cfg, w, logger)
		if err != nil {
			return err
		}
		defer server.Close()
		bound := server.Addresses()
		for _, protocol := range adventure.Keys(bound) {
			fmt.Fprintf(out, "%s: %s\n", protocol, strings.Join(bound[protocol], ", "))
		}
		fmt.Fprintln(out, "Adventure:", w.Title)
		serveCtx, cancelServe := context.WithCancel(ctx)
		defer cancelServe()
		maintenanceDone := make(chan struct{})
		maintenanceError := make(chan error, 1)
		go func() {
			defer close(maintenanceDone)
			if e := store.Maintain(serveCtx); e != nil {
				maintenanceError <- e
				cancelServe()
			}
		}()
		err = server.Serve(serveCtx)
		cancelServe()
		<-maintenanceDone
		select {
		case e := <-maintenanceError:
			if err == nil {
				err = fmt.Errorf("history retention: %w", e)
			}
		default:
		}
		logger.Close()
		// Surface failures while draining the final batch, too.
		select {
		case logErr := <-logger.Errors():
			if err == nil {
				err = logErr
			}
		default:
		}
		if dropped := logger.Dropped(); dropped > 0 {
			fmt.Fprintf(out, "Log queue dropped %d events under load.\n", dropped)
		}
		return err
	default:
		return fmt.Errorf("unknown command %q; use mudpot help", command)
	}
}
