//go:build dev || production

// Song Sleeve for desktop: the Wails shell (D-1) around the shared React UI.
// Build with `make desktop` (Linux needs the webkit2_41 tag: Debian 13 and recent
// Ubuntu ship only WebKitGTK 4.1).
package main

import (
	"context"
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/shell"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/tray"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/update"
)

//go:embed all:frontend/dist
var assets embed.FS

// Set at build time (-ldflags "-X main.version=…"; see the Makefile and the release
// workflow). Without an update URL and key the build doesn't update itself.
var (
	version   = "0.1.0"
	apiBase   = "http://localhost:8088/api"
	updateURL = ""
	updateKey = "" // base64 ed25519 public key of the release signing key
)

func updater(emit func(update.Status)) *update.Updater {
	kind, path := update.Detect()
	key, _ := base64.StdEncoding.DecodeString(updateKey)
	return update.New(update.Config{
		URL: env("SONG_SLEEVE_UPDATE_URL", updateURL), PublicKey: ed25519.PublicKey(key),
		Current: version, Kind: kind, Path: path, Emit: emit,
	})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// dataDir is app-private storage: ~/.local/share/song-sleeve on Linux,
// %LocalAppData%\Song Sleeve on Windows.
func dataDir() string {
	if v := os.Getenv("SONG_SLEEVE_DATA_DIR"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "Song Sleeve")
		}
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "song-sleeve")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "song-sleeve")
}

// sleeveRoot is the user-visible Song Sleeve folder in the home folder (§3).
func sleeveRoot() string {
	if v := os.Getenv("SONG_SLEEVE_FOLDER"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Song Sleeve")
}

func openPath(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	default:
		return exec.Command("xdg-open", p).Start()
	}
}

func main() {
	var appCtx context.Context
	// Set by Quit in the tray and by an update restart: the close that follows
	// must not hide the window.
	var quitting atomic.Bool
	setAppName()
	upd := updater(func(st update.Status) {
		if appCtx != nil {
			wruntime.EventsEmit(appCtx, "update", st)
		}
	})
	audio := []wruntime.FileFilter{{DisplayName: "Music (MP3, AAC, FLAC, WAV)", Pattern: "*.mp3;*.m4a;*.aac;*.mp4;*.flac;*.wav;*.MP3;*.M4A;*.AAC;*.FLAC;*.WAV"}}
	sh, err := shell.New(shell.Config{
		APIBase:    env("SONG_SLEEVE_API", apiBase),
		Updater:    upd,
		Version:    version,
		DataDir:    dataDir(),
		SleeveRoot: sleeveRoot(),
		// For development without a keyring daemon.
		FileCredentials: os.Getenv("SONG_SLEEVE_FILE_CREDENTIALS") == "1",
		Hooks: shell.Hooks{
			Emit: func(name string, data any) {
				if appCtx != nil {
					wruntime.EventsEmit(appCtx, name, data)
				}
			},
			PickFiles: func(folder bool, title string) ([]string, error) {
				if folder {
					p, err := wruntime.OpenDirectoryDialog(appCtx, wruntime.OpenDialogOptions{Title: title})
					if err != nil || p == "" {
						return nil, err
					}
					return []string{p}, nil
				}
				return wruntime.OpenMultipleFilesDialog(appCtx, wruntime.OpenDialogOptions{Title: title, Filters: audio})
			},
			Confirm: func(title, message string) bool {
				r, err := wruntime.MessageDialog(appCtx, wruntime.MessageDialogOptions{
					Type: wruntime.QuestionDialog, Title: title, Message: message,
					Buttons: []string{"Delete", "Keep for now"}, DefaultButton: "Keep for now", CancelButton: "Keep for now",
				})
				return err == nil && (r == "Delete" || r == "Yes")
			},
			Open: openPath,
			Tray: func(st shell.TrayState) { tray.Update(tray.State(st)) },
			Restart: func() error {
				_, path := update.Detect()
				if err := update.Relaunch(path); err != nil {
					return err
				}
				quitting.Store(true)
				go wruntime.Quit(appCtx)
				return nil
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	err = wails.Run(&options.App{
		Title:            "Song Sleeve",
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: &options.RGBA{R: 0x2D, G: 0x2D, B: 0x2A, A: 255},
		AssetServer:      &assetserver.Options{Assets: dist, Handler: sh},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			sh.Restore(ctx)
			go upd.Run(ctx)
			tray.Start(tray.Handlers{
				Show: func() {
					wruntime.WindowShow(appCtx)
					wruntime.WindowUnminimise(appCtx)
				},
				Quit: func() {
					quitting.Store(true)
					wruntime.Quit(appCtx)
				},
				Action: func(name string) { wruntime.EventsEmit(appCtx, "tray", name) },
			})
		},
		// Closing the window keeps the app (and its listener and playback) running
		// while a tray icon can bring it back; without a tray host it quits.
		OnBeforeClose: func(ctx context.Context) bool {
			if quitting.Load() || !tray.Visible() {
				return false
			}
			wruntime.WindowHide(ctx)
			return true
		},
		OnShutdown: func(context.Context) {
			tray.Stop()
			sh.Shutdown()
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "app.songsleeve.desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				wruntime.WindowUnminimise(appCtx)
				wruntime.Show(appCtx)
			},
		},
		Linux: &linux.Options{ProgramName: "song-sleeve", WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand},
	})
	if err != nil {
		log.Fatal(err)
	}
}
