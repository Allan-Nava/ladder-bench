# Changelog

Formato [Keep a Changelog](https://keepachangelog.com/it/1.1.0/), versioni [SemVer](https://semver.org/lang/it/).

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
