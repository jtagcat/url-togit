package pkg

import (
	"errors"
	"io/fs"
	"io/ioutil"
	"os"
	"path"

	"github.com/gogs/git-module"
)

func GitOpenOrInit(path string) (repo *git.Repository, initted bool, err error) {
	repo, err = git.Open(path)
	if err == nil {
		// is there any git on the path?
		_, lserr := git.LsRemote(path)
		if lserr != nil {
			err = os.ErrNotExist
		}
	}
	if errors.Is(os.ErrNotExist, err) {
		initted = true
		err = git.Init(path)
		if err != nil {
			return
		}
		repo, err = git.Open(path)
		return
	}
	return
}

// writes a file, and adds it
func GitWriteAdd(repo *git.Repository, relpath string, data []byte, perm fs.FileMode) error {
	filepath := path.Join(repo.Path(), relpath)
	err := ioutil.WriteFile(filepath, append(data, []byte("\n")...), perm)
	if err != nil {
		return err
	}
	return repo.Add(git.AddOptions{Pathsepcs: []string{relpath}})
}
