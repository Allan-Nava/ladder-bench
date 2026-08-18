# CLAUDE.md — ladder-bench

**ladder-bench** (`github.com/Allan-Nava/ladder-bench`): CLI Go (MIT) che **misura** una encoding ladder ABR invece di ereditarla. Taglia un clip di riferimento, lo codifica su una griglia (risoluzione × bitrate × encoder), misura ogni punto con **VMAF**, e riporta saturazione per rung, frontiera efficiente (convex hull) e ladder consigliata. Zero-dep a parte `gopkg.in/yaml.v3`; guida `ffmpeg`/`ffprobe` come sottoprocessi.

## Regole di lavoro (SEMPRE)

- **Ogni commit = release taggata `vX.Y.Z`**: nuova sezione in `CHANGELOG.md` (Keep a Changelog, **in italiano**) + `git tag -a vX.Y.Z -m "Release X.Y.Z"`. `minor` per novità sostanziali (nuovi comandi/output/metriche), `patch` per fix. Senza chiederlo.
- **MAI `git push`** — lo fa sempre Allan. MAI `Co-Authored-By` nei commit.
- **Gate prima di chiudere**: `gofmt -l .` vuoto + `go vet ./...` + `go test ./...` + `golangci-lint run` verdi (gli stessi di `.github/workflows/ci.yml`). Serve **golangci-lint v2** (config schema v2): `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`.
- **Mai rete o ffmpeg nei test**: l'orchestrazione si testa con `fakeExec` (scrive i file che ffmpeg avrebbe scritto) e `fakeProber`. Ogni funzione che costruisce argomenti ffmpeg è **pura** apposta — si asserisce sugli argomenti, non sul risultato di un encode.
- **Ogni verifica su ffmpeg reale è manuale e va dichiarata**: la CI non ha libvmaf. Se una modifica tocca il modo in cui si misura, farla girare in locale su un clip vero e dirlo nel messaggio/PR.
- **Lingua = inglese**: codice, commenti, test e **tutto l'output user-facing** (report, help, errori, config d'esempio). **Eccezione: `CHANGELOG.md` in italiano**, come negli altri repo.
- **Todo → `BACKLOG.md`** (sorgente unica, id stabili `LB-n`). Niente TODO sparsi nei commenti.
- **Documenta sempre tutto, nello stesso commit**: `docs/` **è** il sito pubblicato (<https://allan-nava.github.io/ladder-bench/>), quindi ogni cosa user-facing che cambia — un comando, un flag, un campo del report, una soglia, una metrica — si aggiorna lì insieme al codice, non dopo. Le pagine sono l'unica copia: il sito le rende, non le duplica. **Nuova pagina = nuovo file** in `docs/` con front matter `title` / `nav_order` / `nav_blurb` / `description` (niente `:` non quotato dentro `description`), e la sidebar la prende da sola — nessuna lista di navigazione da tenere in pari altrove. I link fra pagine restano Markdown relativo (`method.md`), che funziona sia su github.com sia sul sito; **il testo del link non deve andare a capo** o `jekyll-relative-links` non lo riscrive. I link a file fuori da `docs/` vanno assoluti.
- **Niente numeri inventati nel report**: se una misura manca, il report lo dice (`outside the measured grid`, `still climbing`). Mai estrapolare una curva rate-quality — sostituire le stime è il motivo per cui esiste il tool.

## Comandi

```bash
go build -o ladder-bench ./cmd/ladder-bench
go test ./...                 # tutto senza ffmpeg
go vet ./... && golangci-lint run
./ladder-bench init > ladder-bench.yml
./ladder-bench doctor         # ffmpeg, libvmaf, codec, input, work_dir
./ladder-bench plan           # comandi esatti, senza eseguirli
./ladder-bench run --output markdown
```

Smoke locale con sorgente sintetica (serve ffmpeg con libvmaf):

```bash
ffmpeg -f lavfi -i "testsrc2=size=1920x1080:rate=25" -t 8 -c:v libx264 -crf 18 -pix_fmt yuv420p source.mp4
```

## Architettura

- `internal/config/` — YAML tipato, default, `Validate()`. Decodifica **strict** (`KnownFields(true)`).
- `internal/ffmpeg/` — scoperta binari (`exec.LookPath`), capability (`-filters`/`-encoders`), `Probe` (ffprobe JSON), e le **funzioni pure** che costruiscono gli argomenti: `ReferenceArgs`, `EncodeArgs`, `VMAFArgs`. `Executor` è un'interfaccia (i test iniettano un fake). `ParseVMAFLog` legge `pooled_metrics.vmaf`.
- `internal/bench/` — `Grid` (espansione deterministica), `PrepareReference`, `Run` (pool con semaforo, fail-fast), resume su file esistenti, `Cleanup`.
- `internal/analysis/` — pura aritmetica: `Hull` (frontiera), `Knee` (saturazione), `Gain`, `Recommend` (ladder), `Compare` (vs ladder attuale), interpolazioni.
- `internal/output/` — `Text`, `Markdown`, `JSON` sullo stesso `Report`; nessun calcolo qui.
- `cmd/ladder-bench/` — sottocomandi con `flag` stdlib (niente cobra): `init`, `doctor`, `plan`, `run`, `version`. `version` iniettata via ldflags.

## Trappole note / regole tecniche

- **L'ordine degli input di `libvmaf` è distorted-poi-reference**, e il distorted va **riscalato alla geometria del reference**. Sbagliare l'ordine o misurare a risoluzione nativa **non dà errore**: dà numeri diversi e sbagliati (i rung bassi sembrano ottimi e il confronto fra risoluzioni non significa niente).
- **Il reference si taglia lossless, mai in copy**: il copy taglia solo su keyframe, quindi ogni encode partirebbe da un frame diverso. Normalizzare a `yuv420p` una volta sola, perché i modelli VMAF sono addestrati su 8-bit 4:2:0 e formati misti fanno convertire libvmaf implicitamente.
- **Il nome del clip di riferimento contiene il taglio** (`reference_<start>_<dur>.mkv`) e lo costruisce **una sola funzione** (`bench.ReferencePath`, usata sia da `run` sia da `plan`): un secondo posto che compone quel path è drift garantito, e un clip stantio non ha sintomi.
- **I valori dentro un filtergraph vanno escapati** (`escapeFilter`): un `log_path` con `:` — una data, un drive Windows — chiude l'opzione e il resto del path diventa un flag di libvmaf.
- **`listHas` matcha la colonna del nome**, non l'output intero: "VMAF" compare nelle *descrizioni* dei filtri, quindi un `strings.Contains` direbbe che il build ha filtri che non ha.
- **Il bitrate riportato è quello reale** (byte/durata), non quello richiesto: il rate control non centra mai il target. Da qui `analysis.SnapTolerance` (5%): un rung configurato a 3000k contro un punto che ha misurato 2971k è lo stesso rung, non un rung fuori griglia.
- **Confrontare i totali di due ladder con numero di rung diverso non significa niente**: il risparmio si calcola **rung per rung a parità di qualità misurata**, sommando solo i rung confrontabili (`ComparedRungs`). Il totale della ladder consigliata è informativo — punta a `target_vmaf`, non alla qualità di quella attuale.
- **`Knee` scansiona dall'alto verso il basso**: una curva rate-quality ha rumore, e una scansione dal basso si ferma al primo gradino piatto riportando un ginocchio molto più in basso di quello vero.
- **La coda in discesa dell'hull va tagliata**: un punto che costa di più e misura meno (overshoot del rate control) resterebbe nel risultato e si leggerebbe come "paga di più, vedi meno".
- **`concurrency: 1` è il default voluto**: ffmpeg satura già la macchina e encode paralleli rendono i tempi per punto privi di senso.
- **Un punto rotto ferma il run** (fail-fast con `cancel()`): un buco nella curva non è una risposta più piccola, è una risposta sbagliata.
- **La CI non ha libvmaf** e non deve averlo: i test coprono argomenti, parsing e orchestrazione con fake. Ciò che va verificato con ffmpeg vero si verifica a mano.

## Puntatori

- Backlog: `BACKLOG.md` (`LB-n`) · Config d'esempio: `cmd/ladder-bench/example.yml` (embedded, **unica copia** — `ladder-bench init` la scrive)
- CI: `.github/workflows/ci.yml` (fmt/vet/test/lint + govulncheck) · Release: `.github/workflows/release.yml` + `.goreleaser.yaml` sui tag `v*`
- Documentazione: `docs/` — `index.md` (overview), `configuration.md`, `method.md`, `output.md` (il report + schema JSON), `cli.md`, `ci.md`. Sito: `.github/workflows/pages.yml` renderizza `docs/` con Jekyll a ogni push su `main` che tocca `docs/`. Niente tema e niente Gemfile: `docs/_config.yml`, `docs/_layouts/default.html` e `docs/assets/style.css` sono tutta la macchina, scritti a mano. Per provare il build in locale serve il gem `github-pages` (Jekyll 3.10, lo stesso della CI) e un locale UTF-8: `LANG=en_US.UTF-8 bundle exec jekyll build -s docs -d /tmp/site`
- Repo affini: `~/projects/github.com/checkfleet`, `nats-lens`, `nomad-lens`, `ansible-vars-lens`
