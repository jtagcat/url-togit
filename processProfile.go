package main

import (
	"fmt"
	"path"

	"github.com/jtagcat/go-shared"
	"github.com/zmb3/spotify/v2"
	"gopkg.in/yaml.v2"
)

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
	pftSum := pft.Playlists
	for page := 2; ; page++ {
		err = mc.c.NextPage(mc.ctx, pft)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			errChan <- fmt.Errorf("couldn't get profile playlists for %q: page %q %w", id, page, err)
			return
		}
		pftSum = append(pftSum, pft.Playlists...)
	}
	// can't check len(), basePage is unexported

	var mpl []minProfilePlaylist
	for _, pl := range pftSum {
		mpl = append(mpl, minProfilePlaylist{pl.ID, pl.Name})
	}

	e, err := yaml.Marshal(&profileWithPlaylists{User: pf, Playlists: mpl})
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal profile %q: %w", id, err)
		return
	}
	err = shared.GitWriteAdd(mc.r, path.Join("profiles", id.String()+".yaml"), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit profile %q: %w", id, err)
	}
}
