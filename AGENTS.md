# AGENTS.md — ladder-bench

**ladder-bench**: CLI Go (MIT) che misura una encoding ladder ABR — griglia di encode (risoluzione × bitrate × encoder), punteggio VMAF per punto, report con saturazione, frontiera efficiente e ladder consigliata. Guida `ffmpeg`/`ffprobe`; zero dipendenze a parte `gopkg.in/yaml.v3`.

Questo file definisce le regole operative per gli agent (Copilot, Claude, altri tool AI) quando lavorano in questo repository.

## Regole di lavoro (SEMPRE)

- **Ogni commit = release taggata `vX.Y.Z`**: sezione nuova in `CHANGELOG.md` (Keep a Changelog, in italiano) + `git tag -a vX.Y.Z -m "Release X.Y.Z"`. `minor` per novità, `patch` per fix.
- **MAI `git push`**: lo fa sempre l'utente. MAI `Co-Authored-By` nei commit.
- **Gate prima di chiudere**: `gofmt -l .` vuoto, `go vet ./...`, `go test ./...`, `golangci-lint run` (v2) — tutti verdi.
- **Mai rete né ffmpeg nei test**: usare `fakeExec`/`fakeProber` in `internal/bench`; le funzioni che costruiscono argomenti ffmpeg sono pure e si testano sugli argomenti.
- **Verifiche su ffmpeg reale = manuali e dichiarate**: la CI non ha libvmaf.
- **Lingua**: codice, commenti, test e output user-facing in **inglese**; `CHANGELOG.md` in italiano.
- **Todo → `BACKLOG.md`** (id stabili `LB-n`), niente TODO sparsi.
- **Mai estrapolare o inventare misure**: se un punto manca, il report lo dichiara.

## Comandi

- `go build -o ladder-bench ./cmd/ladder-bench` · `go test ./...` · `go vet ./...` · `golangci-lint run`
- Smoke: `./ladder-bench doctor` → `./ladder-bench plan` → `./ladder-bench run`

## Architettura

`internal/config` (YAML strict + default + validate) · `internal/ffmpeg` (binari, capability, probe, **argomenti come funzioni pure**, parsing log VMAF) · `internal/bench` (griglia, reference, esecuzione, resume) · `internal/analysis` (hull, ginocchio, ladder, confronto — solo aritmetica) · `internal/output` (text/markdown/json) · `cmd/ladder-bench` (`init`, `doctor`, `plan`, `run`, `version`).

## Trappole note / regole tecniche

- `libvmaf` prende **distorted come input 0** e reference come input 1, e il distorted va **riscalato alla risoluzione del reference**: sbagliarlo non dà errore, dà numeri sbagliati.
- Il reference si taglia **lossless** (mai `-c copy`) e normalizzato a `yuv420p`.
- Il path del clip di riferimento lo costruisce solo `bench.ReferencePath` (`run` e `plan` devono coincidere).
- Escapare sempre i valori dentro il filtergraph (`escapeFilter`): un `:` nel `log_path` rompe l'opzione.
- Bitrate riportato = quello reale (byte/durata); `analysis.SnapTolerance` (5%) copre lo scarto dal target.
- Il risparmio si calcola **rung per rung a parità di qualità**, sommando solo i rung confrontabili; i totali di ladder con rung diversi non sono confrontabili.
- `Knee` scansiona dall'alto; la coda in discesa dell'hull va tagliata; `concurrency: 1` è il default voluto; un punto rotto ferma il run.

## Puntatori

`BACKLOG.md` · `CLAUDE.md` (versione estesa di queste regole) · `docs/` · config d'esempio embedded in `cmd/ladder-bench/example.yml`.
