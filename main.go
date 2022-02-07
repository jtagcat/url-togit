package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/gogs/git-module"
	"github.com/jtagcat/go-shared"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
	"gopkg.in/yaml.v2"
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
	gitDir, err := shared.PathResolveTilde(gitDir)
	if err != nil {
		log.Fatal(fmt.Errorf("couldn't resolve path for GITDIR: %v", err))
	}
	repo, rinitted, err := shared.GitOpenOrInit(gitDir)
	if err != nil {
		log.Fatal(fmt.Errorf("error opening git dir: %w", err))
	}

	if rinitted {
		// write empty config
		e, err := yaml.Marshal(&Config{})
		if err != nil {
			log.Fatal(fmt.Errorf("error marshalling empty config: %w", err))
		}
		err = shared.GitWriteAdd(repo, configPath, e, modePerm)
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

	periodRaw := os.Getenv("PERIOD")
	periodInt, err := strconv.Atoi(periodRaw)
	if err != nil {
		log.Fatal(fmt.Errorf("couldn't parse PERIOD: %v", err))
	}
	period := time.Duration(periodInt) * time.Minute

	for period != 0 {
		err := routine(mc)
		if err != nil {
			log.Fatal(err)
		}
		time.Sleep(period)
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
