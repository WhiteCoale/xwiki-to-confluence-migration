# Testdaten

Zwei Skripte erzeugen einen vollständigen Testdatensatz für den Migrator:

| Skript | Erzeugt | Ziel |
| :--- | :--- | :--- |
| `seed_xwiki.py` | 4 Benutzer + 21 xWiki-Seiten in 3 Top-Level-Spaces | lokales xWiki (Docker, `http://localhost:8080`) |
| `make_excel.py` | 3 Eingabe-Excel-Dateien, 29 Datenzeilen | `./input/` |

```bash
pip install requests openpyxl pillow
python testdata/seed_xwiki.py --wipe
python testdata/make_excel.py
```

`--wipe` löscht **nur** die von diesem Skript angelegten Spaces
(`Clientthemen`, `Serverthemen`, `PersoenlicheNotizen`) und lässt `TestSpace`,
`MigrationTest` und `Main` unberührt.

## Struktur in xWiki

Nachgebaut ist eine typische Firmen-IT-Wiki-Struktur. Alle Benutzer,
E-Mail-Adressen und Hostnamen sind fiktiv.

```
Clientthemen/                                   (Überordner)
  PTS Protokolltestsystem auf Techniker Client/ Migrieren, 2 Anhänge, 2 Kommentare
  Hyper-V Host unter Accenture/                 Migrieren
  Mainboard Tausch/                             Migrieren, PNG + CSV
  Taschenrechner-App reparieren Windows 10/     Löschen
  Energieoptionen beim Betrieb .../             Löschen
  Notebook Uebergabe/                           nicht in der Excel-Liste, Umlaut-Titel
  Windows/                                      (Unterordner)
    aktuelle Windows 10 Enterprise Iso .../     Löschen
  VMWare Client/                                (Unterordner)
    VMWare Side Channel Mitigation/             Migrieren
  Drucker/                                      (Unterordner)
    Installation                                Duplikat-Titel 1/2
Serverthemen/                                   (Überordner)
  Toolbox Installationsanleitung/               Migrieren
  Wsus/                                         (Unterordner)
    Server verschwindet aus WSUS                Terminal-Seite (ohne WebHome)
    Patchen der Windows Server mittels Wsus/     Migrieren, Log-Anhang
  Backup/                                       (Unterordner)
    Installation                                Duplikat-Titel 2/2
  Monitoring/                                   nicht in der Excel-Liste
PersoenlicheNotizen/                            hidden = true
  VersteckteSeite                               hidden = true
```

## Abdeckung der Anforderungen

| Anforderung | Testfall |
| :--- | :--- |
| Status „Migrieren“ / „Löschen“ aus Spalte A | 3 × Löschen, Rest Migrieren; nicht migrierte müssen im Report stehen |
| System-/Metadatenseiten nicht migrieren | `XWiki/XWikiPreferences` steht bewusst in `03_Sonderfaelle.xlsx` |
| Überordner werden normale Seiten mit Kindern | `Clientthemen/WebHome`, `Windows/WebHome`, `Wsus/WebHome`, … |
| Versteckte Benutzerseiten mitnehmen | Space `PersoenlicheNotizen`, beide Seiten `hidden = true` |
| Alle Felder übernehmen | Tags, Kommentare, Versionshistorie (bis 4 Versionen, Autor ≠ letzter Bearbeiter), Anhänge, Titel |
| Vor-/Nachname statt Benutzer-ID | vier Benutzer mit gepflegtem Profil (Anna Mueller, Thomas Berger, Petra Klein, Jonas Wagner); Seiten werden als der jeweilige Benutzer geschrieben, Revisionen von einem anderen |
| Formatierung, Farbe, Bilder | Überschriften, Tabellen, Listen, `{{code}}`, `{{info}}`/`{{warning}}`/`{{error}}`, farbiger Text (`(% style="color:…" %)`), interne + externe Links, eingebettete PNGs |
| Doppelte Seitennamen ausschließen | zwei Seiten mit dem Titel „Installation“ in verschiedenen Spaces |
| Report | Zeilen ohne Seite, Seiten ohne Zeile, Dubletten, widersprüchlicher Status, leerer/unbekannter Status, Müllzeile |

## Sonderfälle in `03_Sonderfaelle.xlsx`

| Zeile | Zweck |
| :--- | :--- |
| `Clientthemen/Citrix Receiver Altinstallation/WebHome` | steht in der Liste, existiert nicht in xWiki |
| `Mainboard Tausch` (2 ×) | Dublette über zwei Dateien hinweg, davon eine mit widersprüchlichem Status |
| `  migrieren ` | Kleinschreibung + führende/nachlaufende Leerzeichen |
| leerer Status | Status nicht gepflegt |
| `unklar` | unbekannter Statuswert |
| alle Spalten = „Migrieren“ | Müllzeile, wie in der Beispieldatei in Zeile 5 |
| `XWiki/XWikiPreferences` | Systemseite, darf trotz „Migrieren“ nicht migriert werden |

Seiten, die in **keiner** Excel-Datei stehen: `Clientthemen/Notebook Uebergabe`
und `Serverthemen/Monitoring`.
