package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"time"

	"github.com/go-yaml/yaml"
	"github.com/gogs/git-module"
	"github.com/jtagcat/spotify-togit/pkg"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

var (
	configPath    = "config.yaml"
	modePerm      = fs.FileMode(0660)
	dirModePerm   = fs.FileMode(0770)
	committerName = "spotify-togit"
)

type Config struct {
	Playlists []spotify.ID //`yaml:"Playlists,omitempty"`
	Profiles  []spotify.ID
}

func spotifyInit(ctx context.Context) *spotify.Client {
	config := &clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_ID"),
		ClientSecret: os.Getenv("SPOTIFY_SECRET"),
		TokenURL:     spotifyauth.TokenURL,
	}
	token, err := config.Token(ctx)
	if err != nil {
		log.Fatalf("couldn't get token: %v", err)
	}
	httpClient := spotifyauth.New().Client(ctx, token)
	return spotify.New(httpClient)
}

func repoInit() (repo *git.Repository) {
	// open repo
	gitDir := os.Getenv("GITDIR")
	if gitDir == "" {
		log.Fatal("GITDIR not set")
	}
	gitDir, err := pkg.PathResolveTilde(gitDir)
	if err != nil {
		log.Fatal(fmt.Errorf("couldn't resolve path for GITDIR: %v", err))
	}
	repo, rinitted, err := pkg.GitOpenOrInit(gitDir)
	if err != nil {
		log.Fatal(fmt.Errorf("error opening git dir: %w", err))
	}

	if rinitted {
		// write empty config
		e, err := yaml.Marshal(&Config{})
		if err != nil {
			log.Fatal(fmt.Errorf("error marshalling empty config: %w", err))
		}
		err = pkg.GitWriteAdd(repo, configPath, e, modePerm)
		if err != nil {
			log.Fatal(fmt.Errorf("error writing empty config: %w", err))
		}
		err = repo.Commit(&git.Signature{Name: committerName, Email: "", When: time.Now()}, "init with empty config")
		if err != nil {
			log.Fatal(fmt.Errorf("error committing empty config: %w", err))
		}
	}

	return repo
}

type mainCtx struct {
	ctx context.Context
	c   *spotify.Client
	r   *git.Repository
}

func main() {
	ctx := context.Background()
	c := spotifyInit(ctx)
	r := repoInit()
	mc := mainCtx{ctx, c, r}

	err := routine(mc)
	if err != nil {
		log.Fatal(err)
	}
}

func routine(mc mainCtx) error {
	configRaw, err := os.ReadFile(path.Join(mc.r.Path(), configPath))
	if err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}
	config := Config{}
	err = yaml.Unmarshal(configRaw, &config)
	if err != nil {
		return fmt.Errorf("error unmarshalling config: %w", err)
	}

	for _, dir := range []string{"playlists", "profiles"} {
		err = os.Mkdir(path.Join(mc.r.Path(), dir), dirModePerm)
		if err != nil && errors.Is(os.ErrExist, err) {
			return fmt.Errorf("error creating dir %q: %w", dir, err)
		}
	}

	errChan := make(chan error)
	go func() {
		for e := range errChan {
			log.Print(e)
		}
	}()
	// not launching goroutines for rate-limiting sanity
	for _, playlist := range config.Playlists {
		processPlaylist(mc, errChan, playlist)
	}
	for _, profile := range config.Profiles {
		processProfile(mc, errChan, profile)
	}

	return mc.r.Commit(&git.Signature{Name: committerName, Email: "", When: time.Now()}, "routine run")
}

type exportedPlaylist struct { // spotify.FullPlaylist with modifications
	spotify.SimplePlaylist `yaml:"meta,omitempty"`
	Description            string `yaml:"description,omitempty"`
	spotify.Followers      `yaml:"followers,omitempty"`
	Tracks                 []minPlaylistTrack `yaml:"tracks,omitempty"`
}
type minPlaylistTrack struct {
	AddedAt string   `yaml:"added_at,omitempty"`
	AddedBy string   `yaml:"added_by,omitempty"` // other values empty(?)
	IsLocal bool     `yaml:"is_local,omitempty"`
	Track   minTrack `yaml:"track,omitempty"`
}
type minTrack struct {
	ID           spotify.ID             `yaml:"id,omitempty"`
	Name         string                 `yaml:"name,omitempty"`
	Artists      []spotify.SimpleArtist `yaml:"artists,omitempty"`
	ExternalURLs map[string]string      `yaml:"external_urls,omitempty"`
}

func processPlaylist(mc mainCtx, errChan chan<- error, id spotify.ID) {
	pl, err := mc.c.GetPlaylist(mc.ctx, id, spotify.Fields("id,name,collaborative,images,owner(id,display_name),public,snapshot_id,description,followers"))
	if err != nil {
		errChan <- fmt.Errorf("couldn't get playlist %q: %w", id, err)
		return
	}

	plt, err := mc.c.GetPlaylistTracks(mc.ctx, id, // display_name is not included in the response
		spotify.Fields("next,items(added_at,added_by(id,display_name),is_local,track(!album))")) // can't exclude more than 1 item? !available_markets
	if err != nil {
		errChan <- fmt.Errorf("couldn't get playlist tracks for %q: %w", id, err)
	}

	for page := 1; ; page++ {
		err = mc.c.NextPage(mc.ctx, plt)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			errChan <- fmt.Errorf("couldn't get playlist tracks for %q: page %q %w", id, page, err)
		}
	}

	var pltMin []minPlaylistTrack
	for _, t := range plt.Tracks {
		pltMin = append(pltMin, minPlaylistTrack{AddedAt: t.AddedAt, AddedBy: t.AddedBy.ID, IsLocal: t.IsLocal,
			Track: minTrack{ID: t.Track.ID, Name: t.Track.Name, Artists: t.Track.Artists, ExternalURLs: t.Track.ExternalURLs}})
	}

	e, err := yaml.Marshal(&exportedPlaylist{SimplePlaylist: pl.SimplePlaylist, Description: pl.Description, Followers: pl.Followers, Tracks: pltMin})
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal playlist %q: %w", id, err)
		return
	}

	err = pkg.GitWriteAdd(mc.r, path.Join("playlists", id.String()+".yaml"), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit playlist %q: %w", id, err)
	}
}

type profileWithPlaylists struct {
	*spotify.User `yaml:"profile,omitempty"`
	Playlists     []minProfilePlaylist `yaml:"playlists,omitempty"`
}

type minProfilePlaylist struct { // spotify.SimplePlaylist with modifications
	ID   spotify.ID `json:"id"`
	Name string     `json:"name"`
}

func processProfile(mc mainCtx, errChan chan<- error, id spotify.ID) {
	pf, err := mc.c.GetUsersPublicProfile(mc.ctx, id)
	if err != nil {
		errChan <- fmt.Errorf("couldn't get profile %q: %w", id, err)
		return
	}

	pft, err := mc.c.GetPlaylistsForUser(mc.ctx, string(id)) // can't use fields
	if err != nil {
		errChan <- fmt.Errorf("couldn't get profile playlists for %q: %w", id, err)
	}
	for page := 1; ; page++ {
		err = mc.c.NextPage(mc.ctx, pft)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			errChan <- fmt.Errorf("couldn't get profile playlists for %q: page %q %w", id, page, err)
		}
	}
	var mpl []minProfilePlaylist
	for _, pl := range pft.Playlists {
		mpl = append(mpl, minProfilePlaylist{pl.ID, pl.Name})
	}

	e, err := yaml.Marshal(&profileWithPlaylists{User: pf, Playlists: mpl})
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal profile %q: %w", id, err)
		return
	}
	err = pkg.GitWriteAdd(mc.r, path.Join("profiles", id.String()+".yaml"), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit profile %q: %w", id, err)
	}
}
