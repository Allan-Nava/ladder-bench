# Changelog

Formato [Keep a Changelog](https://keepachangelog.com/it/1.1.0/), versioni [SemVer](https://semver.org/lang/it/).

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
