package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gogs/git-module"
	"github.com/jtagcat/spotify-togit/pkg"
	"github.com/jtagcat/util/retry"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	modePerm      = fs.FileMode(0o660)
	committerName = "url-togit"
)

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
	repo, _, err = pkg.GitOpenOrInit(gitDir)
	if err != nil {
		log.Fatal(fmt.Errorf("error opening git dir: %w", err))
	}

	return repo
}

type mainCtx struct {
	ctx      context.Context
	r        *git.Repository
	urlStr   string
	fileName string
}

func main() {
	ctx := context.Background()
	r := repoInit()
	urlStr := os.Getenv("URL")
	_, err := url.ParseRequestURI(urlStr)
	if err != nil {
		log.Fatal(fmt.Errorf("couldn't parse URL: %v", err))
	}
	fileName := os.Getenv("FILENAME")
	if fileName == "" || fileName == ".git" {
		log.Fatal(fmt.Errorf("FILENAME must not be empty or \".git\""))
	}
	mc := mainCtx{ctx, r, urlStr, fileName}

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
	var Body []byte

	err := retry.OnError(wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    3,
	}, func() (bool, error) {
		res, err := http.Get(mc.urlStr)
		if err != nil {
			return true, err
		}
		if res.StatusCode > 299 {
			return true, fmt.Errorf("Non-OK status code: %s", res.Status)
		}
		defer res.Body.Close()

		Body, err = io.ReadAll(res.Body)
		return true, err
	})
	if err != nil {
		return err
	}

	err = pkg.GitWriteAdd(mc.r, mc.fileName, Body, modePerm)
	if err != nil {
		return err
	}

	return mc.r.Commit(&git.Signature{Name: committerName, Email: "", When: time.Now()}, "routine run")
}
