# Backlog — ladder-bench

Sorgente unica dei todo. Id stabili `LB-n`; spuntare, non cancellare.

Roadmap a milestone. **M1** è la v0.1 (fatta): misurare una griglia e leggerne la curva. **M2** rende il risultato difendibile (metriche standard, riproducibilità). **M3** lo porta dove si decide (CI, confronto fra run). **M4** allarga i domini (per-scena, live, hardware).

## M1 — Misurare (v0.1) ✅

- [x] **LB-1 — Config YAML tipata**: griglia (risoluzione × bitrate), encoder multipli, clip, soglie di analisi, ladder attuale. Decodifica strict, default, `Validate()` con messaggi che dicono cosa fare. _(v0.1.0)_
- [x] **LB-2 — Pipeline di misura**: reference lossless tagliato una volta, encode con rate control cappato e GOP fisso, VMAF con upscale alla geometria del reference, log JSON letto e pooled. _(v0.1.0)_
- [x] **LB-3 — Analisi**: ginocchio per rung (VMAF per +10% di bitrate), frontiera efficiente (convex hull), ladder consigliata spaziata per qualità percepita, confronto con la ladder attuale a parità di qualità. _(v0.1.0)_
- [x] **LB-4 — Output**: `text` (terminale), `markdown` (PR/wiki/job summary), `json` (ogni misura). _(v0.1.0)_
- [x] **LB-5 — Comandi di sicurezza**: `doctor` (ffmpeg, libvmaf, codec, input, work_dir), `plan` (i comandi esatti senza eseguirli), `init` (config commentata). _(v0.1.0)_
- [x] **LB-6 — Resume**: i punti già misurati si riusano, `--force` li rifà. Il nome del reference contiene il taglio. _(v0.1.0)_

## M2 — Rendere il risultato difendibile (~v0.2)

- [x] **LB-7 — BD-rate**: differenza di bitrate a parità di qualità fra due encoder/preset sull'intervallo comune (Bjøntegaard). È il numero che si porta in una discussione "vale la pena passare ad AV1?". _(v0.2.0)_
- [x] **LB-8 — Metriche affiancate**: PSNR e SSIM insieme a VMAF nello stesso passaggio (`libvmaf` le espone come feature), più il VMAF **harmonic mean** già letto ma non ancora mostrato — è la colonna che smaschera i clip con pochi secondi rotti. _(v0.3.0)_
- [x] **LB-9 — Percentili per-frame**: il log per-frame è già scritto; esporre p1/p5 dei frame peggiori. Un rung con media 93 e p1 a 70 non è un rung da 93. _(v0.5.0)_
- [ ] **LB-10 — Intervallo di confidenza sul clip**: più clip di riferimento dallo stesso sorgente (`clips:` invece di `clip:`) e riporto della dispersione. Una ladder scelta su 30 secondi fortunati è una ladder scelta a caso.
- [x] **LB-11 — Riproducibilità**: nel report le versioni di ffmpeg/libvmaf/encoder e l'hash della config, così un run vecchio si può replicare o scartare consapevolmente. _(v0.5.0)_
- [x] **LB-21 — Coda di stderr più lunga sul punto rotto**: `tail(stderr, 8)` taglia via la riga della libreria dell'encoder, che è dove sta la causa vera; sopra restano solo le otto righe di cascata di ffmpeg. Visto dal vivo con SVT-AV1: `Svt[error]: Max Bitrate only supported with CRF mode` finiva appena fuori dalla finestra. _(v0.5.0)_

## M3 — Portarlo dove si decide (~v0.3)

- [ ] **LB-12 — `compare` fra run**: due file JSON → cosa è cambiato (curve, ladder, frontiera). È il modo di accorgersi che un aggiornamento di ffmpeg ha spostato la qualità.
- [ ] **LB-13 — Gate CI**: `--exit-on-regression` con soglia, per far fallire una pipeline quando la ladder consigliata peggiora rispetto a un baseline committato.
- [ ] **LB-14 — Export ladder**: la ladder consigliata come snippet pronto (HLS master playlist, DASH adaptation set, JSON per il transcoder) invece di una tabella da ricopiare.
- [x] **LB-22 — Immagine Docker**: `ghcr.io/allan-nava/ladder-bench` con ffmpeg+libvmaf dentro, multi-arch, utente non root, `/work` come workdir. Toglie di mezzo il pezzo difficile del setup: libvmaf è un'opzione di build e **nessun pacchetto di distribuzione la abilita**. _(v0.4.0)_
- [ ] **LB-15 — Grafico**: SVG della curva rate-quality con la frontiera evidenziata, allegabile a una PR. Nessuna dipendenza: SVG scritto a mano.

## M4 — Più domini (~v0.4)

- [ ] **LB-16 — Encoder hardware**: NVENC/QSV/VideoToolbox nella griglia, con l'avvertenza esplicita che il confronto qualità/bitrate con un software encoder non è alla pari.
- [ ] **LB-17 — Per-shot / per-scena**: rilevare i cambi scena e riportare la curva per segmento. È il passo verso il per-title vero.
- [ ] **LB-18 — Profilo live**: griglia con `-preset` bassi e vincoli di latenza, per le ladder di eventi live dove il preset lento non è un'opzione.
- [ ] **LB-19 — Modello 4K / mobile**: selezione del modello VMAF (`vmaf_4k_v0.6.1`, phone model) coerente con lo schermo di consumo dichiarato, oggi possibile solo scrivendo la stringa a mano.
- [ ] **LB-20 — Rate control per encoder**: `maxrate`/`bufsize` sono cablati per tutti, ma non tutti li accettano — SVT-AV1 rifiuta il cap fuori da CRF (`Max Bitrate only supported with CRF mode`), quindi oggi l'encoder AV1 più usato non entra in griglia senza aggirare i default. Serve una forma di rate control per famiglia di encoder, mantenendo il vincolo che rende il numero onesto: un rung che sfonda la banda dichiarata non è consegnabile.
