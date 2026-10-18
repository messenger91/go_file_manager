package main

import (
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
	"go_file_manager/internal/server"
)

//go:embed all:web
var embeddedWeb embed.FS

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		logger.Fatalf("web assets: %v", err)
	}

	srv := server.New(cfg, files.New(cfg.HomeDir), webFS, logger)
	addr := ":" + cfg.Port

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Printf("Go File Manager rooted at %q, listening on http://localhost%s (user %q)", cfg.HomeDir, addr, cfg.Admin)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server: %v", err)
	}
}
