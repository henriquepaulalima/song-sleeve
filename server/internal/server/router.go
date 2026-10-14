// Package server wires every module into the Gin router (§6).
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/henriquepaulalima/song-sleeve/server/internal/accounts"
	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/catalog"
	"github.com/henriquepaulalima/song-sleeve/server/internal/deletion"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/media"
	"github.com/henriquepaulalima/song-sleeve/server/internal/mutations"
	"github.com/henriquepaulalima/song-sleeve/server/internal/operations"
)

func New(d *app.Deps) *gin.Engine {
	if !d.Cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(httpx.RequestIDMiddleware(), httpx.Logger(), httpx.Recover(), httpx.CORS(d.Cfg.AllowedOrigins))
	if len(d.Cfg.TrustedProxies) == 0 {
		_ = r.SetTrustedProxies(nil)
	} else if err := r.SetTrustedProxies(d.Cfg.TrustedProxies); err != nil {
		panic(err)
	}
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	acc := accounts.New(d)
	cat := catalog.New(d)
	ops := operations.New(d)
	del := deletion.New(d)

	// The web reaches the API through its own origin at /api (the Vercel rewrite in
	// production, the Vite proxy locally); installed apps call it directly.
	api := r.Group("/api", httpx.ClientVersion(d.Cfg.MinClientVersion))
	api.GET("/media-policy", func(c *gin.Context) { c.JSON(http.StatusOK, media.CurrentPolicy()) })
	// The terms version registration must accept (R-AUTH-10).
	api.GET("/terms", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"version": d.Cfg.TermsVersion}) })

	pub := api.Group("/auth", httpx.NoStore())
	pub.POST("/register", acc.Register)
	pub.POST("/login", acc.Login)
	pub.POST("/password/forgot", acc.Forgot)
	pub.POST("/password/reset/validate", acc.ValidateReset)
	pub.POST("/password/reset", acc.Reset)

	// Restricted to the cleanup credential (§6 account deletion).
	api.POST("/account-deletion/status", del.Status)
	api.POST("/account-deletion/ack", del.Ack)

	early := api.Group("", accounts.AuthenticateAllowOutdatedTerms(d))
	early.POST("/auth/logout", acc.Logout)
	early.GET("/me", acc.GetMe)
	early.POST("/me/terms", acc.AcceptTerms)

	me := api.Group("/me", accounts.Authenticate(d))
	me.PATCH("", acc.PatchMe)
	me.GET("/summary", acc.Summary)
	me.POST("/password", acc.ChangePassword)
	me.GET("/sessions", acc.ListSessions)
	me.DELETE("/sessions/:id", acc.RevokeSession)
	me.POST("/sessions/revoke-all", acc.RevokeAll)
	me.PUT("/devices/:deviceID", acc.PutDevice)
	me.POST("/account-deletion", del.Request)
	api.POST("/auth/reauthenticate", accounts.Authenticate(d), acc.Reauthenticate)

	me.GET("/mutations/:requestID", mutations.StatusHandler(d))

	me.GET("/catalog/snapshot", cat.Snapshot)
	me.GET("/catalog/changes", cat.Changes)
	me.GET("/events", d.Hub.SSE)

	me.GET("/tracks", cat.ListTracks)
	me.PATCH("/tracks/:id", cat.PatchTrack)
	me.DELETE("/tracks/:id", ops.DeleteTrack)
	me.PUT("/library/songs/order", cat.MoveLibrarySong)
	me.GET("/queue-ids", cat.QueueIDs)

	sync := me.Group("", accounts.RequireCloudSync())
	sync.POST("/tracks", ops.CreateTrack)
	sync.POST("/tracks/:id/replacement", ops.Replacement)
	sync.GET("/operations/:id", ops.GetOperation)
	sync.POST("/operations/:id/upload-grant", ops.UploadGrant)
	sync.POST("/operations/:id/heartbeat", ops.Heartbeat)
	sync.POST("/operations/:id/uploaded", ops.Uploaded)
	sync.POST("/operations/:id/cancel", ops.Cancel)
	sync.POST("/content/:contentID/download-grant", ops.DownloadGrant)
	sync.PUT("/sync-scopes/:type", cat.SetScope("enabled"))
	sync.PUT("/sync-scopes/:type/:id", cat.SetScope("enabled"))
	sync.POST("/sync-scopes/:type/pause", cat.SetScope("paused"))
	sync.POST("/sync-scopes/:type/:id/pause", cat.SetScope("paused"))
	me.GET("/sync-scopes", cat.ListScopes)

	me.GET("/artists", cat.ListArtists)
	me.GET("/artists/:id/songs", cat.ArtistSongs)
	me.PATCH("/artists/:id", cat.RenameArtist)
	me.DELETE("/artists/:id", cat.DeleteArtist)

	me.GET("/playlists", cat.ListPlaylists)
	me.POST("/playlists", cat.CreatePlaylist)
	me.PATCH("/playlists/:id", cat.RenamePlaylist)
	me.DELETE("/playlists/:id", cat.DeletePlaylist)
	me.POST("/playlists/:id/songs", cat.AddPlaylistSongs)
	me.DELETE("/playlists/:id/songs", cat.RemovePlaylistSongs)
	me.PUT("/playlists/:id/songs/order", cat.MovePlaylistSong)

	me.GET("/groups", cat.ListGroups)
	me.POST("/groups", cat.CreateGroup)
	me.PATCH("/groups/:id", cat.RenameGroup)
	me.DELETE("/groups/:id", cat.DeleteGroup)
	me.POST("/groups/:id/playlists", cat.AddGroupPlaylists)
	me.DELETE("/groups/:id/playlists", cat.RemoveGroupPlaylists)
	me.PUT("/groups/:id/playlists/order", cat.MoveGroupPlaylist)

	me.GET("/preferences/library-view", cat.GetPreferences)
	me.PUT("/preferences/library-view", cat.PutPreferences)

	return r
}
