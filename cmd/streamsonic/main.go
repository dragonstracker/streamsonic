package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	"streamsonic/internal/api"
	"streamsonic/internal/config"
	"streamsonic/internal/events"
	"streamsonic/internal/session"
	"streamsonic/internal/stream"
)

var version = "1.2.4"

const defaultConfig = `# streamsonic configuration
http {
  port 8080;
}

# edit_auth admin admin;

# dvr main {
#   root /var/lib/streamsonic/dvr;
#   expiration 604800;
#   storage_limit 50000000000;
# }

# stream channel-1 {
#   input tshttp://example.com/stream.ts;
#   segment_duration 6;
#   segment_count 6;
#   dvr main;
# }
`

func main() {
	configPath := flag.String("config", "streamsonic.conf", "path to the configuration file")
	dvrRoot := flag.String("dvr", "", "default DVR root directory")
	addr := flag.String("http", "", "listen address override, for example :8081")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("streamsonic %s\n", version)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	root := resolveDVRRoot(cfg, *configPath, *dvrRoot)

	bus := events.NewBus(cfg.Sinks)
	registry := session.NewRegistry()
	manager := stream.NewManager(cfg, bus, registry, root)
	store := api.NewStore(cfg, *configPath)
	server := api.NewServer(store, manager, version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	manager.Start(ctx)

	listen := *addr
	if listen == "" {
		listen = fmt.Sprintf(":%d", cfg.HTTP.Port)
	}
	srv := &http.Server{
		Addr:              listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
		manager.Stop()
	}()

	log.Printf("streamsonic %s listening on %s (config %s, dvr %s)", version, listen, *configPath, root)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadFile(path)
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	log.Printf("config %s does not exist, creating a starter file", path)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, []byte(defaultConfig), 0o644); err != nil {
		return nil, err
	}
	cfg, err = config.ParseText(defaultConfig)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	return cfg, nil
}

func resolveDVRRoot(cfg *config.Config, configPath, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg != nil {
		for _, name := range []string{"default", "main"} {
			if d := cfg.DVRs[name]; d != nil && d.Root != "" {
				return d.Root
			}
		}
		names := make([]string, 0, len(cfg.DVRs))
		for name := range cfg.DVRs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if d := cfg.DVRs[name]; d != nil && d.Root != "" {
				return d.Root
			}
		}
	}
	if _, err := os.Stat("/var/lib/streamsonic"); err == nil {
		return "/var/lib/streamsonic/dvr"
	}
	return filepath.Join(filepath.Dir(absPath(configPath)), "dvr")
}

func absPath(p string) string {
	v, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return v
}
