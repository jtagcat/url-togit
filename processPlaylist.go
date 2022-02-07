package main

import (
	"fmt"
	"path"

	"github.com/jtagcat/go-shared"
	"github.com/zmb3/spotify/v2"
	"gopkg.in/yaml.v2"
)

type exportedPlaylist struct { // spotify.FullPlaylist with modifications
	ID            spotify.ID         `yaml:"id,omitempty"`
	Name          string             `yaml:"name,omitempty"`
	IsPublic      bool               `yaml:"public,omitempty"`
	Collaborative bool               `yaml:"collaborative,omitempty"`
	Description   string             `yaml:"description,omitempty"`
	Images        []spotify.Image    `yaml:"images,omitempty"`
	SnapshotID    string             `yaml:"snapshot_id,omitempty"`
	Followers     uint               `yaml:"followers,omitempty"`
	Tracks        []minPlaylistTrack `yaml:"tracks,omitempty"`
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

	pltSum := plt.Tracks
	for page := 1; ; page++ {
		err = mc.c.NextPage(mc.ctx, plt)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			errChan <- fmt.Errorf("couldn't get playlist tracks for %q: page %q %w", id, page, err)
		}
		pltSum = append(pltSum, plt.Tracks...)
	}

	var pltMin []minPlaylistTrack
	for _, t := range pltSum {
		pltMin = append(pltMin, minPlaylistTrack{
			AddedAt: t.AddedAt, AddedBy: t.AddedBy.ID, IsLocal: t.IsLocal,
			Track: minTrack{ID: t.Track.ID, Name: t.Track.Name, Artists: t.Track.Artists,
				ExternalURLs: t.Track.ExternalURLs},
		})
	}

	e, err := yaml.Marshal(&exportedPlaylist{
		ID: pl.ID, Name: pl.Name, IsPublic: pl.IsPublic, Collaborative: pl.Collaborative,
		Images: pl.Images, SnapshotID: pl.SnapshotID, Description: pl.Description,
		Followers: pl.Followers.Count, Tracks: pltMin,
	})
	if err != nil {
		errChan <- fmt.Errorf("couldn't marshal playlist %q: %w", id, err)
		return
	}

	err = shared.GitWriteAdd(mc.r, path.Join("playlists", id.String()+".yaml"), e, modePerm)
	if err != nil {
		errChan <- fmt.Errorf("couldn't commit playlist %q: %w", id, err)
	}
}
