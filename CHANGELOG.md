# Changelog

Formato [Keep a Changelog](https://keepachangelog.com/it/1.1.0/), versioni [SemVer](https://semver.org/lang/it/).

## 0.8.1

### Corretto

- **Una release non pubblica più a metà.** Il push del cask nel tap è l'unico passo cross-repo di una release, quindi l'unico con una credenziale che può guastarsi da sola — e GoReleaser la usa **per ultima**, quando la release GitHub e tutti gli asset sono già pubblici. Con il secret `HOMEBREW_TAP_GITHUB_TOKEN` non ancora impostato, la v0.8.0 è uscita così: release pubblicata, cask mai spinto, `Brew test` saltato dalla sua stessa guardia sul successo, e come unico indizio un `401 Bad credentials` — che è l'aspetto che ha un secret **assente**, perché Actions trasforma un secret mancante in una stringa vuota invece di lamentarsi.

  Ora il job verifica il token prima di costruire qualsiasi cosa: assente, non in grado di leggere il tap, o senza permesso di scrittura (`permissions.push`, perché un token che legge e non scrive fallisce all'ultimo passo esattamente come uno vuoto) → fallisce in venti secondi, senza niente pubblicato e con un messaggio che dice quale secret creare e con che permessi. Una release ora è tutta o niente, invece di una release riuscita con un pezzo silenziosamente mancante.

  Conseguenza pratica: la **v0.8.0 è senza cask**, quindi il primo `brew install --cask Allan-Nava/tap/ladder-bench` che funziona è questo.

## 0.8.0

### Aggiunto

- **Installabile con Homebrew (LB-24).** `brew install --cask Allan-Nava/tap/ladder-bench` su macOS e su Linuxbrew: il cask lo genera GoReleaser a ogni tag `v*` e lo pubblica su `Allan-Nava/homebrew-tap`, accanto agli altri.

  Quattro dettagli decidono se quell'install vale qualcosa:
  - **è un cask, non una formula.** `brews:` è deprecato in GoReleaser v2 (`goreleaser check` fallisce) e una formula che spedisce un binario già compilato sta facendo il lavoro di un cask. Un cask il cui unico artefatto è un `binary` non è macOS-only, quindi Linuxbrew lo installa comunque;
  - **il cask dipende da `ffmpeg`.** libvmaf è un'opzione di compilazione e l'unica che questo tool non può aggirare: senza, il binario sa stampare l'help e nient'altro. La bottle di Homebrew è costruita `--enable-libvmaf`, quindi tirarla dentro come dipendenza è ciò che rende `brew install` un'installazione che *misura*. A runtime vince comunque il primo ffmpeg nel PATH — `ladder-bench doctor` dice quale ha trovato;
  - **la quarantine viene rimossa in postflight.** Il binario non è firmato né notarizzato: senza togliere `com.apple.quarantine` il primo avvio muore con "cannot be opened because the developer cannot be verified", e in quel messaggio non c'è niente che dica che il problema era Gatekeeper e non ladder-bench;
  - **`skip_upload: auto`**, cioè un tag di prerelease non muove il tap: altrimenti un `v1.0.0-rc.1` consegnerebbe a tutti una release candidate via `brew install`, che è l'opposto di cosa serve una rc. La prerelease resta su GitHub, installabile di proposito dall'archivio.

  Il push nel tap è **cross-repo**, quindi il `GITHUB_TOKEN` del job di release non basta: serve il secret `HOMEBREW_TAP_GITHUB_TOKEN` (PAT con `contents:write` sul tap) in questo repository, come già in `checkfleet`.

- **`Brew test`: il tap viene provato installandolo davvero.** Un workflow su macOS arm64 e Intel installa il cask dopo ogni release, a mano e ogni settimana (un tap si rompe in silenzio il giorno che un asset di release viene cancellato), e verifica che il binario riporti una versione vera, che la quarantine sia sparita e che l'ffmpeg tirato dentro abbia il filtro `libvmaf`.

  È anche **l'unico posto automatico dove una misura vera gira**: la CI normale non ha libvmaf e non deve averlo, ma un cask che dipende da ffmpeg di Homebrew ce l'ha, quindi il job codifica e misura due punti su un clip sintetico di due secondi. Un install che consegna un binario incapace di misurare non è un install da riportare verde.

- **La ladder si esporta (LB-14).** `ladder-bench export report.json --format hls|dash|json` la scrive nella forma che un altro sistema accetta, perché una ladder che va ricopiata a mano in un packager è una ladder che prima o poi verrà ricopiata sbagliata. Legge un report, quindi esportare è gratis e si può fare una volta per packager.

  Tre bitrate per rung, perché rispondono a domande diverse: `target_kbps` è quello che ha chiesto la griglia, `peak_kbps` è il **cap** dato all'encode (110% del target) — che è ciò che un `BANDWIDTH` HLS deve dichiarare, essendo un picco — e `kbps` è quello che il file ha misurato, esportato come `AVERAGE-BANDWIDTH`.

  Due cose che si rifiuta di fare:
  - **non inventa `CODECS`.** Porta il profile e il level che l'encoder ha scelto, che qui non si misurano: si conosce il nome dell'encoder e il bitrate. Una stringa indovinata produce una playlist che i player rifiutano in modi che sembrano problemi di contenuto, quindi l'attributo resta fuori e l'export dice come leggerlo da un encode vero (`codecs="TODO"` nel frammento DASH, per lo stesso motivo: un frammento che non si incolla non serve neanche);
  - **non inventa `RESOLUTION`.** La larghezza si deriva dalla geometria del reference come la deriva `scale=-2:H`, e si **omette** quando non c'è geometria da cui derivarla.

  I rung si identificano come `<height>p_<target>k` e non per altezza: una ladder consigliata può avere due rung alla stessa risoluzione, e due voci di playlist che puntano a un URI solo è una playlist che i player non possono usare. Trovato guardando l'output vero: il primo export aveva due `1080p.m3u8`.

- **Il grafico, in SVG scritto a mano (LB-15).** `ladder-bench chart report.json --out chart.svg`: una linea per risoluzione, la frontiera efficiente disegnata sopra come inviluppo, i ginocchi cerchiati e `target_vmaf` tratteggiato — che risponde a "questa griglia ci arriva?" a colpo d'occhio. Nessuna dipendenza, nessun template, niente da installare per renderizzarlo.

  Tre scelte sul disegno: **l'asse dei bitrate è logaritmico**, perché è come si legge un bitrate — il passo da 500k a 1000k è la stessa decisione di quello da 3000k a 6000k e devono occupare la stessa distanza; **non dipinge niente dietro di sé**, perché un SVG che si dipinge il bianco da solo si legge come un buco in un README scuro, e i colori sono mezzitoni che funzionano su entrambi; **la provenienza sta sul grafico** (sorgente, fingerprint della config, timestamp), perché un'immagine viaggia più lontano del report da cui viene, e una curva che non sa cosa l'ha misurata è un ornamento.

## 0.7.0

### Aggiunto

- **`compare` fra due run (LB-12).**

  ```bash
  ladder-bench compare baseline.json current.json
  ```

  Un report dice qual è la ladder oggi; la domanda che viene dopo è sempre se è cambiata — un ffmpeg nuovo, un preset nuovo, un'altra settimana di contenuto. La risposta è **la stessa aritmetica del BD-rate puntata sul tempo invece che su un concorrente**: quanto bitrate serve a questo run per la qualità che consegnava il baseline. Negativo costa meno adesso, positivo costa più.

  Perché quello e non i totali, con numeri veri da una verifica: la ladder consigliata è passata da 3160k a **3121k mentre ogni rung peggiorava** (VMAF 95.83 → 91.67 in cima). Il rate control ha centrato i suoi target come prima; è cambiato cosa comprano quei bit. Confrontare le somme avrebbe chiamato la cosa un miglioramento.

  Tre rifiuti che valgono più delle tabelle:
  - **due run di esperimenti diversi non si confrontano, si dichiarano diversi**. Decide il fingerprint della config: una griglia più larga, un altro target, un clip in più muovono ogni numero senza che niente sia migliorato o peggiorato;
  - **le coordinate e gli encoder che ha solo un run vengono nominati**, non lasciati cadere;
  - **una ladder che ha cambiato forma non si appaia rung per rung**: mettere un 1080p contro un 720p perché stanno allo stesso indice inventerebbe un confronto, quindi si mostrano le due ladder affiancate.

- **Gate CI (LB-13).** `--exit-on-regression` esce **2** quando il run è peggiorato, tenuto distinto dall'1 che vuol dire "lo strumento si è rotto": una regressione è un risultato su cui commentare, un fallimento è qualcosa da andare a riparare, e un codice solo per entrambi rende ambigua ogni build rossa.

  Contano due cose, deliberatamente: la ladder **costa più bit a parità di qualità misurata** oltre `--threshold`, oppure **la griglia non raggiunge più `target_vmaf`** mentre il baseline lo raggiungeva — che non è una questione di grado, il run non risponde più alla domanda. Un run che spende più bit e mostra di più non è una regressione.

  La soglia di default è **2%** e non zero: gli encoder non sono bit-exact fra run e il rate control atterra ogni volta in un punto un po' diverso. Un gate a zero fallisce su quel rumore e viene spento, che è peggio di nessun gate.

  E un gate che **non può** stabilire se qualcosa è peggiorato non passa: con i fingerprint diversi esce 1 dicendo di rimisurare il baseline. Una build verde poggiata su un confronto che non è mai stato fatto è peggio di nessun gate.

### Corretto

- **`compare` accetta i flag anche dopo i nomi dei file.** Il package `flag` della standard library si ferma al primo argomento non-flag, quindi `compare a.json b.json --exit-on-regression` leggeva tre file e zero flag — e **il gate non sarebbe mai scattato**. Nessuno scrive i file per ultimi, quindi il parser ora li accetta per primi (`parseInterspersed`, con un test per ognuna delle quattro disposizioni).

## 0.6.0

M2 chiusa: il risultato è difendibile.

### Aggiunto

- **Più clip di riferimento (LB-10).** `clips:` misura **tutta la griglia** contro più tagli dello stesso sorgente e riporta quanto sono venuti distanti:

  ```yaml
  clips:
    - start: "0s"
      duration: "30s"
    - start: "12m"
      duration: "30s"
    - start: "38m"
      duration: "30s"
  ```

  Serve perché una ladder scelta su trenta secondi fortunati è una ladder scelta a caso, e l'unico modo di sapere se erano fortunati è misurarne altri. Il report guadagna una colonna `SPREAD` — la distanza VMAF fra il clip migliore e il peggiore in quel punto — e un blocco `across N clips` con la disagreement più larga. Quando lo spread supera `ladder_step`, due tagli del **tuo** sorgente non sono d'accordo su un rung per più di un rung intero: la ladder consigliata è la media di due risposte diverse, e la mossa onesta è misurare più contenuto invece di fidarsi più forte del numero.

  Le scelte:
  - **tutto a valle gira sulla curva aggregata** (ginocchio, frontiera, ladder, BD-rate): un run su tre tagli è una risposta diversa e migliore, non tre curve da riconciliare;
  - **medie per ciò da cui si disegna una curva, minimi per ciò che descrive una coda**. Bitrate e VMAF si mediano; `MIN`, `P5` e `P1` prendono il clip **peggiore**, perché mediare le code nasconderebbe il taglio che è andato a pezzi — che è esattamente il taglio per cui si misurano più clip;
  - **le misure per clip non si buttano**: `results[]` resta per-clip e `analysis[]` è l'aggregato, così uno spread si può sempre risalire a quale taglio ha prodotto quale estremo;
  - **`clip:` e `clips:` sono mutuamente esclusivi**, e lo stesso taglio due volte è un errore: dimezzerebbe la dispersione apparente del punto su cui cadono entrambi.

  Verificato su un sorgente costruito con tre tagli deliberatamente diversi — colore piatto, pattern di dettaglio, rumore puro: lo stesso rung 720p misura VMAF 97, 76 e 15, cioè **81.95 VMAF di spread in un punto solo**, e l'avviso scatta. Un clip solo avrebbe riportato quello che gli capitava, senza un accenno all'esistenza degli altri due.

### Modificato

- **`references` al posto di `reference` nel JSON**, come lista: un run ha un clip di riferimento per taglio. Un run a clip singolo ha una lista da un elemento, che è la forma che aveva prima.
- **I nomi dei file portano il clip solo quando ce n'è più di uno**, quindi una work dir a clip singolo continua a valere dopo l'aggiornamento; passare a più clip rinomina quei file e li rimisura. È la stessa regola del rinominare un encoder: un run su tre tagli non è il run su uno che c'era prima.
- **La stima di disco di `plan` misura ogni job sulla durata del *suo* clip.** Prima leggeva `clip.duration`, che con `clips:` è zero: la stima diventava "unknown" proprio sul run che ne ha più bisogno.
- **`plan` stampa un taglio per clip** e marca ogni encode col clip a cui appartiene, altrimenti i comandi non si possono associare a niente.

## 0.5.0

### Aggiunto

- **Percentili per-frame (LB-9).** Colonne `P5` e `P1`: il 5° e il 1° percentile dei punteggi per frame, cioè i momenti peggiori del clip — quelli che una media è fatta per assorbire. Escono dalla sezione per-frame che ogni log libvmaf reale scrive già, quindi **nessun punto viene rimisurato** per averli.

  Si leggono a **rango più vicino** (il valore in posizione ceil(p/100 · N) sui frame ordinati), non interpolati: un percentile interpolato è un punteggio che nessun frame ha ricevuto, ed è esattamente il tipo di numero che questo tool si rifiuta di stampare in ogni altro punto. Due conseguenze da sapere: `n_subsample` li rende più grossolani, perché coprono i frame **misurati** e non tutti; e su un clip corto P1 e P5 cadono sullo stesso frame, che è onesto e non rotto — venti frame non distinguono un primo percentile da un quinto.

  Perché serviva, con numeri veri: su un clip con **un secondo rotto** in mezzo, il rung da 800k misura VMAF **70.15** e P1 **16.35**. La media da sola lo avrebbe dichiarato accettabile.

- **Riproducibilità (LB-11).** Ogni report dice cos'è che l'ha misurato: la **riga di versione di ffmpeg** verbatim come l'ha stampata il binario (distribuzioni e build statiche la scrivono in modi diversi, e citare quello che ha detto batte conservare la nostra interpretazione), **ogni versione di libvmaf che ha scritto uno dei log** — una lista, perché una griglia ripresa può mescolarne due e il report lo dichiara invece di mediarci sopra — e un **fingerprint SHA-256 della config risolta**.

  Il fingerprint è sulla config **dopo i default**, così un file che scrive esplicitamente ciò che un altro lascia implicito produce lo stesso hash. Work dir, path dei binari, `concurrency` e `keep_encodes` sono esclusi di proposito: cambiano *dove* e *quanto velocemente* gira un run, non *cosa* misura, e due macchine con path diversi devono concordare sull'hash perché serva a qualcosa.

### Corretto

- **La causa vera di un punto rotto non cade più fuori dalla finestra (LB-21).** ffmpeg risponde a un encode fallito con una cascata di conseguenze — una per thread che se ne accorge — e la riga che spiega *perché* sta sopra la cascata. Con una coda fissa di 8 righe era la prima cosa a essere buttata via.

  Ora la coda è di 12 righe **e** le righe in cui una libreria di encoding parla di sé stessa (`Svt[error]:`, `x265 [error]:`, `[libsvtav1 @ 0x…] Error …`) vengono **portate sopra** la coda, con un `…` che segna il salto: chi vede due blocchi sa che uno è stato spostato, chi vede solo una cascata troncata non sa niente. Verificato dal vivo con SVT-AV1, dove `Svt[error]: Instance 1: Max Bitrate only supported with CRF mode` ora arriva a schermo — è il messaggio che serve per capire `LB-20`.

## 0.4.0

### Aggiunto

- **Immagine Docker (LB-22)** — `ghcr.io/allan-nava/ladder-bench`, multi-arch (`linux/amd64`, `linux/arm64`), ~220 MB:

  ```bash
  docker run --rm -v "$PWD:/work" ghcr.io/allan-nava/ladder-bench doctor
  ```

  Esiste per togliere di mezzo il pezzo difficile del setup. libvmaf è un'opzione di compilazione e **nessun pacchetto di distribuzione la abilita**: verificato su Debian 12 (ffmpeg 5.1), Debian 13 (7.1), Ubuntu 24.04 (6.1) e Alpine 3.21 (6.1) — tutti installano un ffmpeg perfettamente sano che non misura niente. Quindi i binari vengono da un build statico che ce l'ha (`mwader/static-ffmpeg`, con libvmaf, x264, x265, SVT-AV1 e VP9), **pinnato per digest della manifest list**: digest e non tag perché un ffmpeg nuovo può spostare i numeri, e quello è un cambiamento che va in un messaggio di commit invece che in quello che il registry serviva quella mattina; della *lista* perché è ciò che tiene l'immagine multi-arch.

  Le scelte che si notano usandola:
  - **`/work` è la workdir e l'utente non è root** (uid 1000, quello che un host Linux monouso assegna per primo). Un run scrive reference, encode e log VMAF dentro `work_dir`, che attraverso il bind mount è una directory di qualcuno: girare da root li lascerebbe di proprietà di root in mezzo al suo progetto. Con `-v "$PWD:/work"` una config chiamata `ladder-bench.yml` si trova senza nemmeno un `--config`;
  - **i sottocomandi sono gli argomenti** (`ENTRYPOINT ["ladder-bench"]`), così `docker run <image> doctor` si legge come la CLI che è; senza argomenti stampa l'usage ed esce 0, invece di uscire 64 su un container a cui nessuno ha chiesto niente;
  - **Alpine e non `scratch`**: una shell è ciò che trasforma "il container ha fatto una cosa strana" in una sessione in cui si può guardare in giro.

- **`.github/workflows/docker.yml`** costruisce l'immagine a ogni push e la pubblica su GHCR sui tag `v*` (`0.4.0`, `0.4`, `latest`). Sui non-tag la carica in locale e ci fa girare **una misura vera** su clip sintetico, con assert su VMAF, bitrate, PSNR e SSIM: è **l'unico job del repo che può misurare davvero**, perché l'immagine si porta libvmaf mentre il runner no. Nessun filtro `paths:` di proposito — un tag punta di solito a un commit già su main, quindi il suo push non porta una lista di file su cui filtrare, e la pubblicazione salterebbe esattamente quando serve.

### Modificato

- **`docs/ci.md`** usa l'immagine invece di chiedere un ffmpeg con libvmaf sul runner, e la invoca con `docker run` per comando e **non** come `container:` del job: un job container deve eseguire il Node.js di GitHub per ogni action JavaScript, `actions/checkout` compresa, ed è un build glibc che su una base musl non parte.
- **Documentazione**: pagina nuova [`docs/docker.md`](https://allan-nava.github.io/ladder-bench/docker/) — perché l'immagine esiste, i tre flag che contano (`-v`, `--user`, `--rm`), cosa c'è dentro, come rifarsela, e cosa cambia il container nella misura: la qualità per bit non cambia (è lo stesso codice), i **tempi** sì, sia per un limite di CPU sia sotto emulazione, e i tempi sono l'unico segnale di costo che il report porta.

## 0.3.0

### Aggiunto

- **Metriche affiancate a VMAF (LB-8).** Ogni report mostra ora la colonna `HMEAN`, la **media armonica** VMAF: gli stessi frame, pesati in modo che i peggiori contino di più. libvmaf la calcola sempre e il codice la leggeva già, ma non finiva da nessuna parte — ed è la colonna che smaschera un clip con pochi secondi rotti, dove la media aritmetica se li assorbe. Quando `HMEAN` sta molto sotto `VMAF` quel rung non è stato buono, è stato buono in media.

- **`vmaf.metrics: [psnr, ssim]`** aggiunge **PSNR (piano Y, dB)** e **SSIM** nello *stesso* passaggio di misura, quindi a una frazione del suo costo: i frame sono già decodificati, riscalati e allineati, e il lavoro in più è aritmetica su pixel che libvmaf ha già in mano. Sono le colonne che chiede chi non si fida di VMAF, e servono proprio a essere in disaccordo: PSNR nota cose che VMAF perdona e perdona cose che VMAF nota.

  Tre scelte:
  - **VMAF resta l'unica cosa su cui si decide.** Ginocchio, frontiera, ladder consigliata e BD-rate vengono da VMAF e da nient'altro: mediare due metriche in un punteggio solo nasconderebbe di quale delle due ti stavi fidando;
  - **il set di nomi è chiuso** (`psnr`, `ssim`). libvmaf espone molte altre feature, ma una metrica che il report non sa etichettare né spiegare è una colonna di numeri su cui nessuno può agire. Un nome sbagliato è un errore che elenca le alternative, mai una richiesta ignorata in silenzio;
  - **PSNR è dichiaratamente il solo piano Y.** "PSNR" senza qualificazione in una discussione sui codec vuol dire di solito luma, e dirlo costa meno che farselo chiedere.

- **Accendere le metriche rimisura i punti già su disco.** Un log VMAF scritto senza PSNR non può essere fatto contenere PSNR, quindi quei punti vengono ricodificati e rimisurati al run successivo — automaticamente, senza `--force`. L'alternativa era riusare il log e stampare una colonna vuota, che si legge come una misura tornata in bianco invece che come una misura mai fatta. Per lo stesso motivo le due colonne **non compaiono affatto** se il run non le ha chieste: una colonna di trattini direbbe "abbiamo guardato e non c'era niente".

### Modificato

- **Nel JSON** ogni punto porta `vmaf_harmonic_mean`, e `psnr_y` / `ssim` quando sono state misurate. Le due chiavi sono **assenti**, non zero, quando non lo sono state: uno PSNR di 0 dB sarebbe una catastrofe, e "non misurato" non è una catastrofe. Solo `kbps` e `vmaf` partecipano all'aritmetica; il resto viaggia perché un rung si giudica su più della sua media.
- **Documentazione**: `configuration.md` (la chiave `metrics`, la tabella dei nomi, l'avvertenza sulla cache), `method.md` (cosa esce da un passaggio solo e perché le metriche extra si riportano ma non si usano), `output.md` (le colonne nuove, la regola del trattino, i campi JSON) e `cli.md` (la rimisura come terza conseguenza di una work dir riusata).

## 0.2.1

Solo documentazione e sito: il binario è identico a 0.2.0.

### Aggiunto

- **Sito di documentazione su GitHub Pages** — <https://allan-nava.github.io/ladder-bench/>. Le pagine sono **gli stessi file Markdown in `docs/`**, non una seconda copia: il sito li rende, e `.github/workflows/pages.yml` lo ripubblica a ogni push su `main` che tocca `docs/`. Niente tema e niente Gemfile — `docs/_config.yml`, `docs/_layouts/default.html` e `docs/assets/style.css` sono tutta la macchina, scritti a mano: un tema remoto è una versione in più che può muoversi sotto una modifica ai doc e rompere una pagina che nessuno ha toccato. La sidebar si costruisce dal front matter delle pagine, quindi **aggiungere una pagina è aggiungere un file** e non c'è nessuna lista di navigazione da tenere in pari altrove. I link fra pagine restano Markdown relativo, così funzionano identici su github.com e sul sito.

- **`docs/output.md` — il report, blocco per blocco.** Ogni colonna della tabella delle misure, ogni frase che la saturazione può stampare (`flattens at`, `already flat`, `still climbing`, `not enough points to tell`) e cosa ciascuna dice di fare alla griglia, come si legge il confronto con la ladder attuale e ogni motivo per cui un rung o un BD-rate può rifiutare di dare un numero. In fondo lo **schema JSON completo**, campo per campo: era l'unico formato di output che non fosse documentato da nessuna parte, ed è quello che un grafico o un archivio devono leggere.

- **`docs/cli.md` — ogni comando e ogni flag.** `init`, `doctor`, `plan`, `run`, `version`: default, cosa fa ciascun flag, cosa finisce nel work dir e cosa sopravvive alla pulizia (il log VMAF sì, l'encode no — il log *è* la misura), quando serve `--force`, cosa lascia un Ctrl-C, e i codici di uscita compreso il `64` di usage.

- **`docs/index.md` — la home del sito**: cosa fa il tool, le quattro risposte che dà, installazione, il primo run in sessanta secondi e la regola che segue sempre (non inventa numeri).

- **Un marchio (`docs/assets/logo.svg`).** È il report disegnato: quattro rung misurati, la curva rate-quality che tracciano e il ginocchio segnato. La curva è una cubica monotona per i vertici delle barre, non un arco a mano libera — una curva che scende fra due punti misurati disegnerebbe un'affermazione che il tool non farebbe mai. Nessun testo (niente da dipendere da un font) e solo bianco su verde, così un file solo serve README chiaro, README scuro, favicon del sito e avatar.

### Modificato

- **`docs/method.md` e `docs/configuration.md`** hanno la sezione BD-rate e la regola dell'ancora (il primo encoder in config) già introdotte in 0.2.0, ora raggiungibili dal sito con anchor stabili.
- **README**: badge della documentazione e sezione *Documentation* che punta alle pagine pubblicate invece che alla sola cartella.
- **`CLAUDE.md` / `AGENTS.md`**: regola operativa nuova — *documenta sempre tutto, nello stesso commit*. `docs/` è il sito, quindi ogni cosa user-facing che cambia si aggiorna lì insieme al codice, non dopo.

## 0.2.0

### Aggiunto

- **BD-rate fra encoder (LB-7).** Con due o più encoder in config, il run finisce con il numero che chiude la discussione "vale la pena passare ad AV1?": quanto bitrate serve allo sfidante per la **stessa qualità misurata**, in una percentuale sola invece che in due curve da confrontare a occhio. Compare in `text`, `markdown` e `json`, per risoluzione e fra le due frontiere efficienti — la riga `frontier` è la risposta a livello di ladder, dove ogni encoder è libero di scegliere la sua risoluzione migliore a ogni bitrate.

  Tre scelte decidono se quel numero significa qualcosa:
  - **si integra `log10(bitrate)` su VMAF**, non il bitrate: le differenze di bitrate sono moltiplicative, e mediarle in lineare farebbe pesare la cima della griglia molto più del fondo;
  - **la media si prende solo sull'intervallo di qualità che entrambi gli encoder hanno raggiunto**, mai sull'unione: fuori dalla sovrapposizione non c'è niente contro cui confrontarsi. Due curve che condividono meno di 1.0 VMAF vengono **rifiutate** invece che stirate per farle combaciare — una percentuale presa su una fessura sembra un verdetto ed è il rumore di due frame;
  - **con quattro punti o più la curva ha il fit cubico ai minimi quadrati, con due o tre viene interpolata** e il report dice quale dei due: una cubica per tre punti non è un fit, è una curva tirata attraverso il rumore. L'asse qualità è centrato prima del fit, perché VMAF sta intorno a 90 e una cubica non centrata perde precisione solo per condizionamento.

  L'**ancora** è il **primo encoder elencato in config** — quello che si manda in onda oggi — così il segno si legge sempre allo stesso modo: negativo = lo sfidante costa meno. Ordinare gli encoder alfabeticamente avrebbe ribaltato il segno di tutto il report il giorno in cui qualcuno rinomina un preset.

### Corretto

- **Un punto rotto ora dice perché.** Il fail-fast cancella il context, quindi gli encode in volo vengono uccisi e quelli in coda dietro il semaforo si sbloccano: gran parte degli errori di un run fermato sono ricaduta del fermo, e `Run` ne riportava uno di quelli. Il messaggio era `context canceled`, cioè niente da debuggare, mentre la coda di stderr di ffmpeg — che il codice raccoglieva già — non arrivava mai a schermo. Ora l'errore restituito è **quello che ha fermato il run**, e un context cancellato senza nessun punto fallito resta quello che è: un Ctrl-C.

## 0.1.0

Prima release: misurare una encoding ladder invece di ereditarla.

- **Pipeline di misura (LB-1, LB-2).** `ladder-bench run` taglia un clip di riferimento **lossless** dal sorgente, lo codifica su una griglia (risoluzione × bitrate × encoder) e misura ogni punto con **VMAF**. Tre dettagli decidono se i numeri significano qualcosa, e sono cablati qui: il reference non si taglia in `-c copy` (taglierebbe solo su keyframe, e ogni encode partirebbe da un frame diverso), il distorted viene **riscalato alla geometria del reference** prima di essere valutato (VMAF modella uno spettatore su schermo pieno: un 360p va giudicato come l'immagine ingrandita che vede davvero), e il rate control è **cappato** con `maxrate`/`bufsize` (un rung che sfonda la banda dichiarata è un rung che il player abbandona, quindi misurarlo senza cap darebbe una qualità non consegnabile).

- **Analisi (LB-3).** Tre risposte, tutte aritmetica pura sui punti misurati:
  - **saturazione per rung** — il ginocchio oltre cui ogni gradino guadagna meno di `knee_gain` punti VMAF per +10% di bitrate. La scansione va **dall'alto verso il basso**: una curva reale ha rumore, e dal basso ci si ferma al primo gradino piatto riportando un ginocchio molto più giù di quello vero;
  - **frontiera efficiente** — il convex hull superiore di tutti i punti di tutte le risoluzioni, cioè "a questo bitrate, quale risoluzione si vede meglio". La coda in discesa viene tagliata: un punto che costa di più e misura meno si leggerebbe come "paga di più, vedi meno";
  - **ladder consigliata** — rung presi dalla frontiera (mai bitrate interpolati) e spaziati di ~6 VMAF, all'incirca una differenza appena percepibile, invece del solito dimezzamento del bitrate che ammucchia rung indistinguibili in cima e lascia un salto in fondo.

- **Confronto con la ladder attuale (LB-3).** Per ogni rung configurato: che qualità consegna oggi, e quanto costa la **stessa** qualità sulla frontiera. Il totale somma **solo i rung confrontabili**: mettere su un piatto un rung che la griglia non ha mai misurato inventerebbe un risparmio da una misura mancante. Il totale della ladder consigliata resta informativo — punta a `target_vmaf`, non alla qualità di quella attuale, e due ladder con numero di rung diverso non sono confrontabili per somma.

- **Comandi di sicurezza (LB-5).** `doctor` verifica ffmpeg, la presenza di **libvmaf** (è un'opzione di build: scoprirlo dopo un'ora di encode è il modo peggiore), i codec configurati, il sorgente e il work dir. `plan` stampa i comandi ffmpeg esatti senza eseguirne nessuno — i comandi sono il metodo, e un benchmark di cui non si leggono i parametri non è discutibile. `init` scrive una config commentata (copia unica, embedded nel binario).

- **Resume (LB-6).** I punti già misurati si riusano, quindi una griglia interrotta riparte da dove si era fermata; `--force` rifà tutto. Il nome del clip di riferimento contiene il taglio, così cambiare `clip:` produce un file diverso invece di riusare in silenzio quello vecchio.

- **Output (LB-4).** `text` per il terminale, `markdown` per una PR o un job summary, `json` con ogni misura. I renderer scrivono attraverso un writer che ricorda il primo errore: un report troncato da un disco pieno non deve uscire con exit 0.
