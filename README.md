# Hosted at https://pattyrich.github.io/github-pages/

This project was bootstrapped with [Create React App](https://github.com/facebook/create-react-app).

`npm i && npm start` should be all you need to start working with the project. 

`server/server.py` will need to be run if you want to work on the `bingo` section locally. There is a requirements.txt in that folder. 

## `bingo` CLI

`cli/` is a Go binary that drives a board over the same API this app uses, for
scripts and agents rather than people: `--json` output, meaningful exit codes,
no prompts. Build it with `cd cli && go build -o bingo .`, or take a
`bingo-{os}-{arch}` binary off a release.

Board credentials are kept in `~/.bingo/boards.json` by `board create`, so every
other command only needs `--board`.

```
bingo board create --name mesoscape-pvm --teams "Raiders,Slayers" --size 5x5
bingo board show   --name mesoscape-pvm
bingo teams rename --board mesoscape-pvm --teams "Alpha,Beta"

bingo tile add    --board mesoscape-pvm --title "Twisted Bow" --points 10
bingo tile list   --board mesoscape-pvm
bingo tile edit   --board mesoscape-pvm --tile "Twisted Bow" --points 12
bingo tile remove --board mesoscape-pvm --tile "Example Tile"
bingo tile mark   --board mesoscape-pvm --tile "Twisted Bow" --team Raiders
bingo tile unmark --board mesoscape-pvm --tile "Twisted Bow" --team Raiders
```

### Addressing a tile

Every command that touches an existing tile takes either `--tile` (exact title,
case-insensitive) or `--col` with `--row`. Prefer the position from a script: a
title can repeat on a board, and a repeated title is refused rather than guessed
at. Positions are `[column,row]`, the same order `tile list` prints.

### `tile edit`

```
bingo tile edit --board B (--tile T | --col C --row R) \
  [--title NEW] [--points N] [--description D] [--image URL | --image-file PATH]
```

Only the fields you name change; everything else on the tile is sent back
untouched, so editing the points never costs the tile its title or its art.
`--image ""` clears the art. `--image-file` downscales the file to 512px and
hands it to the board host as a data URI, which the host re-hosts permanently:
prefer it over `--image` for anything a person uploaded, because a Discord
attachment URL expires in about two weeks.

### `tile remove`

```
bingo tile remove --board B (--tile T | --col C --row R) [--force]
```

Returns the cell to exactly the shape the board host gives one that never held a
tile, so `tile add` will reuse it.

A tile some team has already scored is refused unless you pass `--force`.
Removing one takes the tile off the board but leaves its points in the
standings, and nothing then records why the totals stopped adding up. Run
`tile unmark` first and the score goes with it.

### Exit codes

`0` success, `1` the board host said no, `3` the caller addressed something that
is not there or is not allowed (unknown board, unresolvable tile, a scored tile
without `--force`).
