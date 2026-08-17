# xWiki to Confluence Migration Tool

Go-Werkzeug, das Inhalte aus xWiki (v17.x+) nach Confluence Cloud migriert.

## Die drei Schritte

| Schritt | Modus | Was passiert |
| :--- | :--- | :--- |
| 1 | `export` | Alle Inhaltsseiten aus xWiki herunterladen und lokal ablegen |
| 2 | `import` | Lokale Ablage nach Confluence Cloud hochladen |
| 3 | `report` | Excel-Report über die gesamte Migration schreiben |

`--mode all` führt alle drei aus. Export und Import sind entkoppelt: der Export
läuft offline im Netz des xWiki, der Import auf einer Maschine mit Internet.

## Eingabe: Ordner mit Excel-Dateien

In `--input-dir` (Standard `./input`) liegen beliebig viele `.xlsx`-Dateien.
Maßgeblich sind zwei Spalten:

* **Spalte A `Status`** – `Migrieren` oder `Löschen`. `Löschen` heißt: nicht
  migrieren; in xWiki wird nichts gelöscht.
* **Spalte H `path`** – der xWiki-Pfad, z. B. `Clientthemen/Windows/WebHome`.

Die übrigen Spalten (`Kommentarfeld`, `title`, `initial_creator`, `created_date`,
`last_editor`, `last_change_date`, `link`) werden für den Report übernommen.
Die Spaltenzuordnung erfolgt über die Kopfzeile, sonst über die feste Position.

Verhalten in Sonderfällen:

| Fall | Ergebnis |
| :--- | :--- |
| Seite in keiner Liste | nicht migriert, Grund „Nicht in der Eingabeliste enthalten“ |
| Status leer oder unbekannt | nicht migriert, Grund „Status nicht auswertbar“ |
| Dieselbe Seite mehrfach gelistet | einmal migriert, Hinweis im Report |
| Widersprüchlicher Status | konservativ **nicht** migriert, Konflikt im Report |
| Zeile ohne passende xWiki-Seite | im Report als „In xWiki nicht gefunden“ |
| Zeile mit identischem Inhalt in allen Spalten | als unbrauchbar gemeldet |

Ohne Excel-Dateien im Eingabeordner werden alle Inhaltsseiten migriert.

## Was migriert wird

* **Struktur**: Die xWiki-Seitenhierarchie wird 1:1 aus den Seitenreferenzen
  abgeleitet. Auch „Überordner“ (`WebHome` eines Unterbereichs) werden zu
  normalen Confluence-**Seiten**, unter denen die Unterseiten hängen – es
  werden keine Confluence-Ordner angelegt.
* **Verschachtelte Bereiche** und **versteckte Seiten** sind eingeschlossen.
* **Inhalt**: Die Seite wird von xWiki gerendert (`?xpage=plain`) und nach
  Confluence-Storage-Format übersetzt. Erhalten bleiben Überschriften, Fett/
  Kursiv, Farben, Tabellen, Listen, Code-Blöcke (als Code-Makro), Info-/
  Warnung-/Fehler-Boxen (als Confluence-Makros), Bilder und Links. Interne
  xWiki-Links werden auf die neuen Confluence-Seiten umgebogen.
* **Metadaten**: Tags werden zu Labels, Kommentare zu Confluence-Kommentaren,
  Anhänge werden hochgeladen. Ersteller, letzter Bearbeiter, Zeitstempel,
  Version und xWiki-Link stehen in einem aufklappbaren Herkunfts-Panel auf jeder
  Seite; die Versionshistorie folgt am Seitenende.
* **Benutzernamen**: Statt der xWiki-Benutzer-ID (`XWiki.amueller`) wird
  **Vorname Nachname** ausgewiesen (`Anna Mueller`). Die Namen kommen direkt aus
  dem `XWiki.XWikiUsers`-Objekt des Benutzerprofils und werden bereits beim
  Export aufgelöst – der Import läuft dadurch weiterhin ohne xWiki-Zugriff.
  Betroffen sind Ersteller, letzter Bearbeiter, Kommentar- und
  Versionsautoren. Ist im Profil kein Name hinterlegt, bleibt die ID stehen.
* **Owner**: Der letzte xWiki-Bearbeiter wird über die Confluence-Benutzersuche
  aufgelöst und als Seiten-Owner gesetzt. Findet sich kein passendes Konto,
  bleibt der API-Benutzer Owner – die Originalangaben stehen dann im
  Herkunfts-Panel. Confluence Cloud lässt den Versionsautor nicht setzen.
* **Nicht migriert** werden xWiki-System-, Standard- und Metadatenseiten
  (Spaces wie `XWiki`, `Mail`, `Panels`, `Crypto`, `WikiManager`, `Code`-
  Unterbereiche sowie `*Class`, `*Sheet`, `*Template`, `WebPreferences` …).

### Doppelte Seitentitel

Confluence erlaubt pro Space keine doppelten Titel. Vor dem ersten Schreibzugriff
werden deshalb alle Zieltitel vergeben: kollidiert ein Titel mit einer anderen
migrierten Seite oder mit einer bereits im Space vorhandenen Seite, hängt das
Tool eine stabile Kennung an, z. B. `Installation (a1b2c3d4)`. Die Kennung ist
aus der xWiki-Referenz abgeleitet und damit bei jedem Lauf identisch.

## Ziel in Confluence

Alles wird im **bestehenden** Space unter einer Seite `Import` angelegt
(Titel über `--root-page` änderbar). Existiert der Space nicht, bricht das Tool
ab – mit `--create-space` legt es ihn an.

## Setup

1. `.env` im Projektverzeichnis anlegen:
   ```env
   XWIKI_URL=http://your-xwiki-host:8080
   XWIKI_USER=Admin
   XWIKI_PASSWORD=secret

   CONFLUENCE_URL=https://your-site.atlassian.net/wiki
   CONFLUENCE_USER=your-email@example.com
   CONFLUENCE_TOKEN=your-api-token
   CONFLUENCE_SPACE_KEY=KEY_DES_BESTEHENDEN_SPACE
   ROOT_PAGE=Import
   ```
   `CONFLUENCE_SPACE_KEY` ist der **Key** aus der URL
   (`/wiki/spaces/<KEY>/overview`), nicht der Anzeigename.
2. Flags überschreiben `.env`-Werte.

## Ablauf

### Schritt 1: Export (offline-fähig)

```bash
./migration.exe --mode export
```

Erzeugt `./export` mit `index.json` und je Seite einen Ordner mit
`content.html` (gerendert), `source.xwiki` (Originalsyntax), `metadata.json`
und `attachments/`.

### Schritt 2 + 3: Import und Report

```bash
./migration.exe --mode import
```

Der Import schreibt `migration-state.json` mit der Zuordnung xWiki-Seite →
Confluence-Seite. Ein erneuter Lauf aktualisiert dadurch die eigenen Seiten,
statt sie zu duplizieren. Die Datei liegt bewusst **außerhalb** von `./export`,
damit ein neuer Export sie nicht mitlöscht.

Geht sie doch verloren, ist das nicht kritisch: jede migrierte Seite trägt ein
Label `xwiki-id-<kennung>`, über das der Import seine eigenen Seiten wiederfindet
und die Zuordnung neu aufbaut. Im Anschluss entsteht automatisch der Report.

Nur den Report (zeigt ohne Confluence-Zugriff, was passieren würde):

```bash
./migration.exe --mode report
```

### Report

`export/migration-report-<zeitstempel>.xlsx` mit zwei Blättern:

* **Migration** – eine Zeile je Seite: Status (`migriert` / `nicht migriert` /
  `Fehler`), Grund, xWiki- und Confluence-Titel, xWiki-Pfad, Ersteller, letzter
  Bearbeiter, Zeitstempel, Version, versteckt, Tags, Anhänge, Kommentaranzahl,
  xWiki-Link, Confluence-Link sowie Excel-Datei, -Status und -Kommentar.
  Farblich hinterlegt und mit Autofilter.
* **Zusammenfassung** – Kennzahlen des Laufs.

## Offline-Nutzung (portabler Export)

1. Auf einer Maschine mit Internet bauen:
   ```bash
   go build -mod=vendor -o migration.exe .
   ```
   (oder `build.bat`)
2. `migration.exe` und `.env` auf die Offline-Maschine kopieren.
3. Dort `./migration.exe --mode export` ausführen. Es werden nur die vendorten
   Abhängigkeiten genutzt; außer dem lokalen xWiki wird nichts kontaktiert.

## Testdaten

`testdata/` enthält Skripte, die ein lokales xWiki mit realistischen Inhalten
und passende Eingabe-Excel-Dateien befüllen – siehe
[testdata/README.md](testdata/README.md).

## Command Line Flags

| Flag | Beschreibung | Standard |
| :--- | :--- | :--- |
| `--mode` | `all`, `export`, `import`, `report` | `all` |
| `--export-dir` | Lokale Ablage | `./export` |
| `--input-dir` | Ordner mit den Eingabe-Excel-Dateien | `./input` |
| `--report` | Pfad des Reports | `<export-dir>/migration-report-<zeit>.xlsx` |
| `--xwiki-url` | Basis-URL des xWiki | `http://localhost:8080` |
| `--confluence-space-key` | Key des bestehenden Ziel-Space | – |
| `--root-page` | Seite, unter der importiert wird | `Import` |
| `--create-space` | Ziel-Space anlegen, falls nicht vorhanden | `false` |
| `--skip-spaces` | Zusätzlich auszuschließende Spaces | – |
| `--state-file` | Zuordnung xWiki-Seite → Confluence-Seite | `./migration-state.json` |

## Tests

```bash
go test ./...
```

Die Tests decken Konvertierung, Seitenfilter, Hierarchieableitung,
Statusauswertung und Titelvergabe ab. Liegt ein lokaler Export vor, wird
zusätzlich jede exportierte Seite konvertiert und auf wohlgeformtes XML geprüft.

---
*Erstellt im Rahmen des Siemens xWiki-zu-Confluence-Migrationsprojekts.*
