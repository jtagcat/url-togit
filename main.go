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

	log.Fatal(routine(mc))
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
		err = os.Mkdir(path.Join(mc.r.Path(), dir), modePerm)
		if err != nil && errors.Is(os.ErrExist, err) {
			return fmt.Errorf("error creating dir %q: %w", dir, err)
		}
	}

	errChan := make(chan error)
	for _, playlist := range config.Playlists {
		go processPlaylist(mc, errChan, playlist)
	}
	for _, profile := range config.Profiles {
		go processProfile(mc, errChan, profile)
	}

	for e := range errChan {
		log.Print(e)
	}

	return mc.r.Commit(&git.Signature{Name: committerName, Email: "", When: time.Now()}, "routine run")
}

// generics wen?
func processPlaylist(mc mainCtx, errChan chan<- error, id spotify.ID) {
	pl, err := mc.c.GetPlaylist(mc.ctx, id)
	if err != nil {
		errChan <- fmt.Errorf("couldn't get playlist %q: %w", id, err)
		return
	}

	e, err := yaml.Marshal(pl)
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal playlist %q: %w", id, err)
		return
	}
	err = pkg.GitWriteAdd(mc.r, path.Join("playlists", id.String()), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit playlist %q: %w", id, err)
	}
}

func processProfile(mc mainCtx, errChan chan<- error, id spotify.ID) {
	pl, err := mc.c.GetUsersPublicProfile(mc.ctx, id)
	if err != nil {
		errChan <- fmt.Errorf("couldn't get profile %q: %w", id, err)
		return
	}

	e, err := yaml.Marshal(pl)
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal profile %q: %w", id, err)
		return
	}
	err = pkg.GitWriteAdd(mc.r, path.Join("profiles", id.String()), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit profile %q: %w", id, err)
	}
}
