package unique

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var (
	_nameBin  string
	_nameData string
	_nameConf string
	_nameDB   string

	config Config
)

func initPath() {
	path, err := filepath.Abs(os.Args[0])
	panicIfError(os.Args[0], err)
	dir, bin := filepath.Split(path)
	_nameBin = filepath.Join(dir, bin)
	_nameData = _nameBin + "_db"
	os.Mkdir(_nameData, 0o755)

	_nameConf = filepath.Join(_nameData, "config.json")
	_nameDB = filepath.Join(_nameData, "db.json")

	data, err := os.ReadFile(_nameConf)
	if os.IsNotExist(err) {
		config = Config{
			Dirs: []Dir{{
				Dir:      ".",
				KeepMode: "none",
			}},
			Ignore:   []string{".git"},
			Compress: true,
		}
		b, _ := json.MarshalIndent(config, "", "    ")
		panicIfError(_nameConf, os.WriteFile(_nameConf, b, 0o755))
	} else {
		panicIfError(_nameConf, err)
		panicIfError(_nameConf, json.Unmarshal(data, &config))
	}

	_, err = os.Stat(_nameDB)
	if os.IsNotExist(err) {
		data, _ = compress([]byte{'[', ']'})
		err = os.WriteFile(_nameDB, data, 0o755)
	}
	panicIfError(_nameDB, err)
}

// Dir config
// "full", path keep full dir name
// "base", path keep base dir name
// "none", path keep none dir name
type Dir struct {
	Dir      string `json:"dir"`
	KeepMode string `json:"keep_mode"`
}

type Config struct {
	Dirs     []Dir    `json:"dirs"`   // scan dirs, default is '.'
	Ignore   []string `json:"ignore"` // ignore all dir name, default is '.git'
	Compress bool     `json:"compress"`
}

type FileItem struct {
	Path  string    `json:"path"` // primary key
	Sha1  string    `json:"sha1,omitempty"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`

	trim string `json:"-"`
}

func FileIter(dir string, ltrim int, ignore func(string) bool, fileCh chan<- FileItem) {
	entries, err := os.ReadDir(dir)
	panicIfError(dir, err)

	for idx := range entries {
		entry := entries[idx]
		p := filepath.Join(dir, entry.Name())
		if p == _nameBin || p == _nameData {
			continue
		}

		if entry.IsDir() {
			if !ignore(entry.Name()) {
				FileIter(p, ltrim, ignore, fileCh)
			}
		} else if entry.Type().IsRegular() {
			info, err := entry.Info()
			panicIfError(p, err)
			fullPath := filepath.Join(dir, info.Name())
			fileCh <- FileItem{
				trim:  fullPath[:ltrim],
				Path:  fullPath[ltrim:],
				Size:  info.Size(),
				Mtime: info.ModTime(),
			}
		}
	}
}

func Run() {
	initPath()
	fmt.Println("Run   bin:", _nameBin)
	fmt.Println("Run  data:", _nameData)

	var filesLoad []*FileItem
	var filesNew []string
	var filesUpdate []string
	var filesDel []string
	var filesRepeat []struct {
		Sha1 string
		Path string
	}
	mapFilesDel := make(map[string]struct{})

	data, err := os.ReadFile(_nameDB)
	panicIfError(_nameDB, err)
	data, err = decompress(data)
	panicIfError(_nameDB, err)
	panicIfError(_nameDB, json.Unmarshal(data, &filesLoad))

	mapByPath := make(map[string]*FileItem)
	for _, fi := range filesLoad {
		if _, exist := mapByPath[fi.Path]; exist {
			panicIfError(fi.Path, os.ErrExist)
		}
		mapByPath[fi.Path] = fi
		mapFilesDel[fi.Path] = struct{}{}
	}

	fileCh := make(chan FileItem)
	errCh := make(chan any, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				errCh <- p
			} else {
				close(fileCh)
			}
		}()

		mapIgnore := make(map[string]struct{})
		for _, name := range config.Ignore {
			mapIgnore[name] = struct{}{}
		}
		ignore := func(name string) bool {
			_, exist := mapIgnore[name]
			return exist
		}

		for _, dir := range config.Dirs {
			var trim string
			fullDir, errg := filepath.Abs(dir.Dir)
			panicIfError(dir.Dir, errg)
			switch dir.KeepMode {
			case "base":
				trim, _ = filepath.Split(fullDir)
			case "none":
				trim, _ = filepath.Split(filepath.Join(fullDir, "none"))
			}
			fmt.Println("List path:", fullDir)
			fmt.Println("List trim:", trim)
			FileIter(fullDir, len(trim), ignore, fileCh)
		}
	}()

	loadPath := make(map[string]string)
	mapBySize := make(map[int64][]*FileItem)

	done := false
	for !done {
		select {
		case f, ok := <-fileCh:
			if !ok {
				done = true
				continue
			}
			if trim, exist := loadPath[f.Path]; exist {
				panicIfError(filepath.Join(trim, f.Path)+"\n"+
					filepath.Join(f.trim, f.Path), os.ErrExist)
			}
			loadPath[f.Path] = f.trim
			delete(mapFilesDel, f.Path)

			fi, exists := mapByPath[f.Path]
			if !exists {
				filesNew = append(filesNew, f.Path)
				fi = &FileItem{
					Path:  f.Path,
					Size:  f.Size,
					Mtime: f.Mtime,
				}
				mapByPath[f.Path] = fi
			}

			if !fi.Mtime.Equal(f.Mtime) {
				filesUpdate = append(filesUpdate, f.Path)
				fi.Sha1 = ""
				fi.Size = f.Size
				fi.Mtime = f.Mtime
			}

			if fi.Size > 0 {
				mapBySize[fi.Size] = append(mapBySize[fi.Size], fi)
			}

		case err := <-errCh:
			panic(err)
		}
	}

	mapBySha1 := make(map[string][]string)
	for _, files := range mapBySize {
		if len(files) <= 1 {
			continue
		}
		for _, fi := range files {
			if fi.Sha1 == "" {
				fi.Sha1 = calSha1(filepath.Join(loadPath[fi.Path], fi.Path), fi.Size)
			}
			mapBySha1[fi.Sha1] = append(mapBySha1[fi.Sha1], fi.Path)
		}
	}
	for key := range mapBySha1 {
		if len(mapBySha1[key]) <= 1 {
			delete(mapBySha1, key)
		}
	}

	if len(filesNew)+len(filesUpdate)+len(mapFilesDel)+len(mapBySha1) == 0 {
		return
	}
	for path := range mapFilesDel {
		filesDel = append(filesDel, path)
	}
	sort.Strings(filesDel)

	for sha1 := range mapBySha1 {
		filesRepeat = append(filesRepeat, struct {
			Sha1 string
			Path string
		}{
			Sha1: sha1,
			Path: mapBySha1[sha1][0],
		})
		sort.Strings(mapBySha1[sha1])
	}
	sort.Slice(filesRepeat, func(i, j int) bool {
		return filesRepeat[i].Path < filesRepeat[j].Path
	})

	result := filepath.Join(_nameData, time.Now().Format(
		"2006-01-02.15_04_05.000000000")+".txt")
	rf, err := os.Create(result)
	panicIfError(result, err)

	writeLine := func(space, path string) {
		rf.WriteString(space)
		rf.WriteString(path)
		rf.WriteString("\n")
	}

	for key, files := range map[string][]string{
		"New":    filesNew,
		"Update": filesUpdate,
		"Delete": filesDel,
	} {
		for idx, path := range files {
			if idx == 0 {
				rf.WriteString("Files ")
				rf.WriteString(key + ":\n")
			}
			writeLine("  ", path)
		}
	}
	for idx, sha1 := range filesRepeat {
		if idx == 0 {
			rf.WriteString("Files Repeated:\n")
		}
		writeLine("  ", sha1.Sha1)
		for _, path := range mapBySha1[sha1.Sha1] {
			writeLine("    ", path)
		}
	}
	rf.Close()

	filesSave := make([]*FileItem, 0, len(filesLoad))
	for _, fi := range mapByPath {
		if _, exists := mapFilesDel[fi.Path]; !exists {
			filesSave = append(filesSave, fi)
		}
	}
	sort.Slice(filesSave, func(i, j int) bool {
		return filesSave[i].Path < filesSave[j].Path
	})
	data, err = json.MarshalIndent(filesSave, "", "    ")
	panicIfError(_nameDB, err)
	data, err = compress(data)
	panicIfError(_nameDB, err)
	if err = os.WriteFile(_nameDB, data, 0o755); err != nil {
		panicIfError(_nameDB, err)
	}
}

func Decompress(path string) {
	config.Compress = true
	fmt.Println("Compress :", path)

	data, err := os.ReadFile(path)
	panicIfError(path, err)
	data, err = decompress(data)
	panicIfError(path, err)
	panicIfError(path, os.WriteFile(path+".dec.json", data, 0o755))
}

func calSha1(p string, n int64) string {
	file, err := os.Open(p)
	panicIfError(p, err)
	defer file.Close()
	hash := sha1.New()
	_, err = io.CopyN(hash, file, n)
	panicIfError(p, err)
	return hex.EncodeToString(hash.Sum(nil))
}

func panicIfError(path string, err error) {
	if err != nil {
		panic(fmt.Sprintf("path: %s\n%s", path, err.Error()))
	}
}

func compress(pb []byte) ([]byte, error) {
	if !config.Compress {
		return pb, nil
	}
	buffer := new(bytes.Buffer)
	gw := gzip.NewWriter(buffer)
	if _, err := gw.Write(pb); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decompress(cb []byte) ([]byte, error) {
	if !config.Compress {
		return cb, nil
	}
	gr, err := gzip.NewReader(bytes.NewBuffer(cb))
	if err != nil {
		return nil, err
	}
	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, gr); err != nil {
		return nil, err
	}
	if err := gr.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
