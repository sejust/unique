# Unique


## Unique

Find the same files using `sha1` quickly.


## Build

```sh
go build -o unique main/main.go

# windows
GOOS=windows GOARCH=amd64 go build -o unique.exe main/main.go
```


## Usage

- `unique_db` is the directory of database.
- `unique_db/config.json` is the config file.
- `unique_db/db.json` is the database.
- `unique_db/*.txt` is the result.

```txt
./unique
tree
├── unique
├── unique_db
│   ├── 2025-03-27.12_45_28.151155249.txt
│   ├── config.json
│   ├── db.json
└── unique.go
```


## Config

```config
{
    "dirs": [
        {
            "dir": ".",
            "keep_mode": "none"
        }
    ],
    "ignore": [
        ".git"
    ],
    "compress": true
}
```

`keep_mode`:
* "full", path keep full dir name.
* "base", path keep base dir name.
* "none", path keep none dir name.

`compress` compress `db.json` with gzip.


## Decompress

- `unique_db/db.json.dec.json` is readable.

```txt
./unique unique_db/db.json
tree
├── unique
├── unique_db
│   ├── config.json
│   ├── db.json
│   └── db.json.dec.json
```

