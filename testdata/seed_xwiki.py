#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Seed the local xWiki (Docker, http://localhost:8080) with realistic test data
for the xWiki -> Confluence migration tool.

The structure mirrors a typical corporate IT wiki (Clientthemen /
Serverthemen). All users, e-mail addresses and host names are fictional.
It covers every requirement of the migration:

  * "Ueberordner" pages (WebHome of a nested space) that must become plain
    Confluence pages with children below them
  * duplicate page titles across spaces (Confluence forbids these per space)
  * pages that are NOT listed in any input Excel file
  * pages flagged "Loeschen" (must show up in the report as not migrated)
  * hidden pages
  * umlauts / special characters in titles
  * rich formatting: headings, colors, tables, lists, code + info/warning
    macros, internal and external links, images
  * attachments (PNG image, plain text, CSV)
  * tags (-> Confluence labels), comments and multi-version history
    (last editor differs from creator)

Usage:
    python testdata/seed_xwiki.py              # create everything
    python testdata/seed_xwiki.py --wipe       # delete the seeded spaces first
"""

import argparse
import io
import sys
import urllib.parse
from xml.sax.saxutils import escape

import requests

XWIKI_URL = "http://localhost:8080"
AUTH = ("Admin", "admin")

# Top level spaces created by this script. Everything below them is removed
# when --wipe is used, nothing else in the wiki is touched.
SEEDED_SPACES = ["Clientthemen", "Serverthemen", "PersoenlicheNotizen", "RestWriteProbe"]

session = requests.Session()
session.auth = AUTH

# Wiki users with real first/last names. The migrator reads these from the
# XWiki.XWikiUsers object so that the migrated pages show "Vorname Nachname"
# instead of the login id.
USER_PASSWORD = "testpass123"
USERS = [
    ("amueller", "Anna", "Mueller", "anna.mueller@example.com"),
    ("tberger", "Thomas", "Berger", "thomas.berger@example.com"),
    ("pklein", "Petra", "Klein", "petra.klein@example.com"),
    ("jwagner", "Jonas", "Wagner", "jonas.wagner@example.com"),
]


def user_session(login):
    """A session authenticating as one of the seeded users."""
    s = requests.Session()
    s.auth = (login, USER_PASSWORD)
    return s


def create_users():
    """Create the wiki users and give the Admin account a real name too."""
    print("Creating users ...")
    for login, first, last, email in USERS:
        page = "{}/rest/wikis/xwiki/spaces/XWiki/pages/{}".format(
            XWIKI_URL, urllib.parse.quote(login, safe=""))

        profile = ('<?xml version="1.0" encoding="UTF-8"?>'
                   '<page xmlns="http://www.xwiki.org"><title>{} {}</title>'
                   '<syntax>xwiki/2.1</syntax><content>{{{{velocity}}}}'
                   '{{{{/velocity}}}}</content></page>').format(escape(first), escape(last))
        session.put(page, data=profile.encode("utf-8"),
                    headers={"Content-Type": "application/xml; charset=utf-8"})

        # Form-encoded writes need a CSRF token; the XML representation does not.
        obj = ('<?xml version="1.0" encoding="UTF-8"?>'
               '<object xmlns="http://www.xwiki.org"><className>XWiki.XWikiUsers</className>'
               '<property name="first_name"><value>{}</value></property>'
               '<property name="last_name"><value>{}</value></property>'
               '<property name="email"><value>{}</value></property>'
               '<property name="password"><value>{}</value></property>'
               '<property name="active"><value>1</value></property>'
               '</object>').format(escape(first), escape(last), escape(email), USER_PASSWORD)

        resp = session.post(page + "/objects", data=obj.encode("utf-8"),
                            headers={"Content-Type": "application/xml; charset=utf-8"})
        if resp.status_code not in (200, 201, 202):
            # The object already exists on a re-run; update it in place.
            resp = session.put(page + "/objects/XWiki.XWikiUsers/0",
                               data=obj.encode("utf-8"),
                               headers={"Content-Type": "application/xml; charset=utf-8"})

        add_to_group(login)
        print("  {} {:>3}  {} ({} {})".format(
            "OK " if resp.status_code in (200, 201, 202) else "ERR",
            resp.status_code, login, first, last))

    set_admin_name()


def add_to_group(login):
    """Members of XWikiAllGroup may edit pages, which the seeding needs."""
    obj = ('<?xml version="1.0" encoding="UTF-8"?>'
           '<object xmlns="http://www.xwiki.org"><className>XWiki.XWikiGroups</className>'
           '<property name="member"><value>XWiki.{}</value></property></object>').format(login)
    session.post(
        "{}/rest/wikis/xwiki/spaces/XWiki/pages/XWikiAllGroup/objects".format(XWIKI_URL),
        data=obj.encode("utf-8"),
        headers={"Content-Type": "application/xml; charset=utf-8"})


def set_admin_name():
    """The stock Admin account has no first name; give it one."""
    obj = ('<?xml version="1.0" encoding="UTF-8"?>'
           '<object xmlns="http://www.xwiki.org"><className>XWiki.XWikiUsers</className>'
           '<property name="first_name"><value>System</value></property>'
           '<property name="last_name"><value>Administrator</value></property>'
           '</object>')
    session.put("{}/rest/wikis/xwiki/spaces/XWiki/pages/Admin/objects/XWiki.XWikiUsers/0"
                .format(XWIKI_URL), data=obj.encode("utf-8"),
                headers={"Content-Type": "application/xml; charset=utf-8"})


# Which user creates which part of the tree, so that creator and last editor
# differ and the report shows a realistic mix of names.
def author_for(spaces):
    root = spaces[0]
    sub = spaces[1] if len(spaces) > 1 else ""
    if root == "PersoenlicheNotizen":
        return "pklein"
    if root == "Serverthemen":
        return "jwagner" if sub in ("Backup", "Monitoring") else "tberger"
    if sub in ("Drucker", "Notebook Uebergabe"):
        return "jwagner"
    if sub == "Taschenrechner-App reparieren Windows 10":
        return "pklein"
    return "amueller"


def editor_for(author):
    """The last editor is deliberately someone else than the creator."""
    return "jwagner" if author == "amueller" else "amueller"


# --------------------------------------------------------------------------
# REST helpers
# --------------------------------------------------------------------------

def space_path(spaces):
    """['A', 'B'] -> 'spaces/A/spaces/B' with proper URL encoding."""
    return "/".join("spaces/" + urllib.parse.quote(s, safe="") for s in spaces)


def page_url(spaces, page):
    return "{}/rest/wikis/xwiki/{}/pages/{}".format(
        XWIKI_URL, space_path(spaces), urllib.parse.quote(page, safe="")
    )


def put_page(spaces, page, title, content, parent=None, hidden=False, sess=None):
    """Create or update a page. Repeated calls create new versions (history).

    Passing sess writes the page as that user, which is what makes creator and
    last editor differ in the exported metadata.
    """
    sess = sess or session
    xml = ['<?xml version="1.0" encoding="UTF-8"?>', '<page xmlns="http://www.xwiki.org">']
    xml.append("<title>%s</title>" % escape(title))
    xml.append("<syntax>xwiki/2.1</syntax>")
    if parent:
        xml.append("<parent>%s</parent>" % escape(parent))
    if hidden:
        xml.append("<hidden>true</hidden>")
    xml.append("<content>%s</content>" % escape(content))
    xml.append("</page>")

    resp = sess.put(
        page_url(spaces, page),
        data="".join(xml).encode("utf-8"),
        headers={"Content-Type": "application/xml; charset=utf-8"},
    )
    ok = resp.status_code in (200, 201, 202)
    print("  {} {:>3}  {}.{}".format("OK " if ok else "ERR", resp.status_code,
                                     ".".join(spaces), page))
    if not ok:
        print("      " + resp.text[:300])
    return ok


def set_tags(spaces, page, tags, sess=None):
    sess = sess or session
    xml = ['<?xml version="1.0" encoding="UTF-8"?>', '<tags xmlns="http://www.xwiki.org">']
    for t in tags:
        xml.append('<tag name="%s"/>' % escape(t, {'"': "&quot;"}))
    xml.append("</tags>")
    resp = sess.put(
        page_url(spaces, page) + "/tags",
        data="".join(xml).encode("utf-8"),
        headers={"Content-Type": "application/xml; charset=utf-8"},
    )
    if resp.status_code not in (200, 201, 202):
        print("      tags failed ({}): {}".format(resp.status_code, resp.text[:200]))


def add_comment(spaces, page, text, sess=None):
    sess = sess or session
    xml = ('<?xml version="1.0" encoding="UTF-8"?>'
           '<comment xmlns="http://www.xwiki.org"><text>%s</text></comment>' % escape(text))
    resp = sess.post(
        page_url(spaces, page) + "/comments",
        data=xml.encode("utf-8"),
        headers={"Content-Type": "application/xml; charset=utf-8"},
    )
    if resp.status_code not in (200, 201, 202):
        print("      comment failed ({}): {}".format(resp.status_code, resp.text[:200]))


def add_attachment(spaces, page, filename, data, sess=None):
    sess = sess or session
    resp = sess.put(
        page_url(spaces, page) + "/attachments/" + urllib.parse.quote(filename, safe=""),
        data=data,
        headers={"Content-Type": "application/octet-stream"},
    )
    if resp.status_code not in (200, 201, 202):
        print("      attachment {} failed ({}): {}".format(filename, resp.status_code,
                                                           resp.text[:200]))


def delete_space(space):
    """Delete a whole top level space (best effort, used by --wipe)."""
    url = "{}/rest/wikis/xwiki/{}/pages".format(XWIKI_URL, space_path([space]))
    resp = session.get(url, headers={"Accept": "application/json"})
    if resp.status_code != 200:
        return
    # Delete deepest pages first so parents disappear last.
    pages = sorted(resp.json().get("pageSummaries", []),
                   key=lambda p: p["fullName"].count("."), reverse=True)
    for p in pages:
        session.delete("{}/rest/wikis/xwiki/spaces/{}/pages/{}".format(
            XWIKI_URL,
            "/spaces/".join(urllib.parse.quote(s, safe="") for s in p["space"].split(".")),
            urllib.parse.quote(p["name"], safe="")))


def wipe():
    """Delete every page of the seeded spaces.

    Uses /pages rather than /spaces: the space listing omits hidden spaces, so
    walking it would silently leave the hidden test pages behind - and xWiki
    keeps the original creator when such a leftover page is merely updated.
    """
    print("Wiping previously seeded spaces ...")
    resp = session.get(XWIKI_URL + "/rest/wikis/xwiki/pages?number=1000",
                       headers={"Accept": "application/json"})
    if resp.status_code != 200:
        print("  ERR could not list pages")
        return

    targets = []
    for p in resp.json().get("pageSummaries", []):
        root = p["fullName"].split(".")[0]
        if root in SEEDED_SPACES:
            targets.append(p)

    # Deepest pages first so parents disappear last.
    for p in sorted(targets, key=lambda p: p["fullName"].count("."), reverse=True):
        session.delete("{}/rest/wikis/xwiki/{}/pages/{}".format(
            XWIKI_URL, space_path(p["space"].split(".")),
            urllib.parse.quote(p["name"], safe="")))

    print("  cleared: {} Seiten in {}".format(len(targets), ", ".join(SEEDED_SPACES)))


# --------------------------------------------------------------------------
# Binary test assets
# --------------------------------------------------------------------------

def make_png(text, color):
    """Small labelled PNG so image migration is visually verifiable."""
    from PIL import Image, ImageDraw
    img = Image.new("RGB", (480, 200), color)
    draw = ImageDraw.Draw(img)
    draw.rectangle([8, 8, 471, 191], outline=(255, 255, 255), width=3)
    draw.text((28, 90), text, fill=(255, 255, 255))
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


CSV_ASSET = (
    "Hostname;Modell;Standort;Letzte Pruefung\r\n"
    "ACME-NB-0417;Lenovo T14 Gen4;Erlangen;2025-11-04\r\n"
    "ACME-NB-0918;Dell Latitude 5540;Nuernberg;2025-09-22\r\n"
    "ACME-WS-0031;HP Z2 G9;Erlangen;2025-12-01\r\n"
).encode("utf-8")

LOG_ASSET = (
    "2025-11-19 09:31:02 INFO  Starte Protokolltest (PTS) auf Techniker-Client\n"
    "2025-11-19 09:31:44 WARN  Zeitueberschreitung Kanal 3, Wiederholung 1/3\n"
    "2025-11-19 09:33:10 INFO  Alle 12 Telegramme erfolgreich quittiert\n"
).encode("utf-8")


# --------------------------------------------------------------------------
# Page content building blocks (xwiki/2.1 syntax)
# --------------------------------------------------------------------------

RICH_PTS = u"""= PTS Protokolltestsystem auf Techniker Client =

Das **Protokolltestsystem (PTS)** wird auf dem Techniker-Client eingesetzt, um
Feldgeraete ohne Anbindung an die Leitstelle zu pruefen.

{{info}}
Diese Seite wurde zuletzt im Rahmen des Client-Rollouts 2025 aktualisiert.
{{/info}}

== Voraussetzungen ==

* Windows 11 Enterprise, Build 23H2 oder neuer
* Lokale Administratorrechte //waehrend der Installation//
* Freigeschaltete Ports **5001-5010** in der Client-Firewall
** Ausnahme: Standort Erlangen, dort zusaetzlich Port 5020

== Kompatibilitaetsmatrix ==

|= Komponente |= Version |= Status
| PTS Core | 4.8.2 | (% style="color:#2e7d32" %)**freigegeben**(%%)
| PTS Treiberpaket | 2.11 | (% style="color:#ef6c00" %)**Pilot**(%%)
| Legacy-Adapter | 1.4 | (% style="color:#c62828" %)**abgekuendigt**(%%)

== Installation ==

1. Installationspaket aus der Toolbox beziehen
1. Setup mit erhoehten Rechten starten
1. Lizenzdatei ##pts.lic## in ##C:\\ProgramData\\PTS## ablegen

{{code language="powershell"}}
$Paket = "\\\\example\\software\\PTS\\PTS-Core-4.8.2.msi"
Start-Process msiexec.exe -ArgumentList "/i `"$Paket`" /qn" -Wait -Verb RunAs
Get-Service PTSAgent | Restart-Service
{{/code}}

{{warning}}
Der Legacy-Adapter 1.4 darf **nicht** parallel installiert werden - der
PTS-Dienst startet sonst nicht.
{{/warning}}

== Screenshot ==

[[image:pts-oberflaeche.png]]

== Weiterfuehrend ==

* [[Toolbox Installationsanleitung>>doc:Serverthemen.Toolbox Installationsanleitung.WebHome]]
* [[Hersteller-Supportportal>>https://support.example.com/]]
"""

RICH_HYPERV = u"""= Hyper-V Host unter Accenture Clients =

Auf den von Accenture verwalteten Clients ist Hyper-V **nicht** vorinstalliert.
Die Aktivierung erfolgt ueber einen Ticket-Antrag.

== Ablauf ==

1. Ticket in der Kategorie //Client / Virtualisierung// eroeffnen
1. Begruendung und Projektnummer angeben
1. Nach Freigabe: Neustart des Clients einplanen

{{code language="powershell"}}
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
{{/code}}

|= Frage |= Antwort
| Laeuft VMware parallel? | Nein, nur eine Hypervisor-Ebene
| Wer genehmigt? | Standort-IT-Verantwortlicher

{{error}}
Ohne Freigabe wird Hyper-V beim naechsten Compliance-Scan wieder entfernt.
{{/error}}

[[Zurueck zur Uebersicht>>doc:Clientthemen.WebHome]]
"""

RICH_MAINBOARD = u"""= Mainboard Tausch - Rechnerwiederinbetriebnahme =

Nach einem Mainboard-Tausch verliert der Rechner Bitlocker-Bindung und
Domaenenvertrauen. Diese Seite beschreibt die Wiederinbetriebnahme.

== Checkliste ==

* (% style="color:#c62828" %)**Vor dem Tausch**(%%): Bitlocker-Wiederherstellungsschluessel sichern
* Nach dem Tausch: TPM im BIOS aktivieren und loeschen
* Domaenenvertrauen zuruecksetzen

{{code language="powershell"}}
Reset-ComputerMachinePassword -Server dc01.example.com -Credential (Get-Credential)
manage-bde -protectors -add C: -TPM
{{/code}}

== Haeufige Fehlerbilder ==

|= Symptom |= Ursache |= Loesung
| Bitlocker fragt Recovery Key | TPM-Bindung verloren | Protector neu setzen
| Anmeldung "Vertrauensstellung fehlgeschlagen" | Computerkonto ungueltig | Machine Password zuruecksetzen
| Kein Netzwerk | neue MAC, kein DHCP-Reservat | Reservierung anpassen

[[image:mainboard-hinweis.png]]

Anlage: Geraeteliste als CSV im Anhang dieser Seite.
"""

RICH_TOOLBOX = u"""= Toolbox Installationsanleitung =

Die **Toolbox** buendelt alle Standardwerkzeuge fuer Server- und Clientbetrieb.

== Installationsvarianten ==

|= Variante |= Zielgruppe |= Silent-Schalter
| Vollinstallation | Serverbetrieb | ##/qn ADDLOCAL=ALL##
| Minimal | Techniker-Client | ##/qn ADDLOCAL=Core,Diag##

{{code language="bash"}}
msiexec /i Toolbox-Setup.msi /qn ADDLOCAL=Core,Diag /l*v C:\\Temp\\toolbox.log
{{/code}}

{{info}}
Der Installationslog wird fuer Supportfaelle benoetigt - bitte aufbewahren.
{{/info}}

== Nach der Installation ==

1. Lizenzserver eintragen
1. Update-Kanal auf //stable// stellen
1. Funktionstest durchfuehren

[[Wsus Uebersicht>>doc:Serverthemen.Wsus.WebHome]]
"""

RICH_WSUS_MISSING = u"""= Server verschwindet aus WSUS =

Ein Server taucht nach einiger Zeit nicht mehr in der WSUS-Konsole auf, obwohl
der Windows-Update-Dienst laeuft.

== Ursache ==

Geklonte Systeme teilen sich dieselbe **SusClientId**. WSUS ueberschreibt dann
den jeweils zuletzt gemeldeten Eintrag.

== Loesung ==

{{code language="powershell"}}
Stop-Service wuauserv
Remove-ItemProperty -Path "HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\WindowsUpdate" `
    -Name SusClientId, SusClientIdValidation -ErrorAction SilentlyContinue
Start-Service wuauserv
wuauclt /resetauthorization /detectnow
{{/code}}

{{warning}}
Nach dem Zuruecksetzen dauert es bis zu 30 Minuten, bis der Server wieder in der
Konsole erscheint.
{{/warning}}

* Betroffen: alle aus Template //W2019-Base// geklonten Server
* Nicht betroffen: per Setup installierte Systeme
"""

RICH_WSUS_PATCH = u"""= Patchen der Windows Server mittels Wsus =

== Patch-Fenster ==

|= Ring |= Systeme |= Fenster
| Ring 1 | Test- und Laborsysteme | Dienstag 18:00
| Ring 2 | Applikationsserver | Donnerstag 20:00
| Ring 3 | (% style="color:#c62828" %)Produktion(%%) | Samstag 22:00

== Vorgehen ==

1. Snapshot bzw. Sicherung pruefen
1. Zielgruppe in WSUS freigeben
1. Nach dem Neustart Dienste kontrollieren

{{code language="powershell"}}
Get-Service | Where-Object { $_.StartType -eq 'Automatic' -and $_.Status -ne 'Running' }
{{/code}}

{{info}}
Die Freigabe der Updates erfolgt immer **ringweise**, niemals global.
{{/info}}

Ausfuehrliches Protokoll siehe Anhang.
"""

RICH_VMWARE = u"""= VMWare Side Channel Mitigation =

Die Side-Channel-Mitigations (Spectre / Meltdown / L1TF) kosten je nach Last
**10-30 % Performance**. Fuer isolierte Laborhosts koennen sie deaktiviert werden.

{{error}}
Nur fuer Systeme **ohne** Netzanbindung an Produktivumgebungen zulaessig.
Freigabe durch die Security-Abteilung ist zwingend erforderlich.
{{/error}}

|= Einstellung |= Standard |= Labor
| ##hyperthreadingMitigation## | true | false
| ##VMkernel.Boot.hyperthreadingMitigation## | aktiv | deaktiviert

{{code language="bash"}}
esxcli system settings kernel set -s hyperthreadingMitigation -v FALSE
esxcli system shutdown reboot -d 10 -r "Side Channel Mitigation deaktiviert"
{{/code}}

[[Uebergeordnet: VMWare Client>>doc:Clientthemen.VMWare Client.WebHome]]
"""

RICH_ISO = u"""= aktuelle Windows 10 Enterprise Iso downloaden =

{{warning}}
**Veraltet.** Windows 10 Enterprise ist seit Oktober 2025 ausser Support.
Diese Seite wird nicht migriert (Status "Loeschen" in der Eingabeliste).
{{/warning}}

Der Download erfolgte frueher ueber das Volume Licensing Service Center.

1. Anmeldung am VLSC
1. Produkt //Windows 10 Enterprise LTSC// waehlen
1. Sprache **Deutsch** und Architektur **x64** waehlen

Nachfolger: siehe Windows-11-Rollout im Client-Portal.
"""

RICH_TASCHENRECHNER = u"""= Taschenrechner-App reparieren Windows 10 =

{{warning}}
Historischer Eintrag - Status "Loeschen". Dient als Testfall fuer Seiten, die im
Report als //nicht migriert// erscheinen muessen.
{{/warning}}

{{code language="powershell"}}
Get-AppxPackage *WindowsCalculator* | Reset-AppxPackage
{{/code}}

Falls das fehlschlaegt, hilft eine Neuregistrierung ueber ##Add-AppxPackage##.
"""

RICH_ENERGIE = u"""= Energieoptionen beim Betrieb mit Stromversorgung =

{{warning}}
Status "Loeschen" - inzwischen zentral per Gruppenrichtlinie geregelt.
{{/warning}}

|= Einstellung |= Netzbetrieb |= Akku
| Bildschirm aus | 15 min | 5 min
| Energiesparmodus | nie | 15 min
| Festplatte abschalten | nie | 20 min
"""

RICH_UMLAUT = u"""= Notebook-Übergabe & -Rückgabe (Größe/Prüfung) =

Diese Seite testet **Umlaute, Sonderzeichen und kaufmaennisches Und** im Titel
sowie im Inhalt: äöüÄÖÜß, «Anführungszeichen», 50 % Auslastung, 3 °C.

== Übergabeprotokoll ==

|= Prüfpunkt |= Ergebnis
| Gehäuse äußerlich unbeschädigt | ja
| Netzteil & Dockingstation vollständig | ja
| Datenträger verschlüsselt | ja

{{info}}
Diese Seite ist in **keiner** Eingabe-Excel enthalten und muss im Report als
"nicht in Liste" auftauchen.
{{/info}}
"""

RICH_DRUCKER_INST = u"""= Installation =

**Duplikat-Testfall 1 von 2.** Dieselbe Überschrift existiert unter
##Serverthemen.Backup.Installation##. In Confluence sind doppelte Seitentitel je
Space nicht erlaubt - der Migrator muss das vorab aufloesen (z. B. per Präfix
oder UUID im Titel).

== Netzwerkdrucker einbinden ==

{{code language="powershell"}}
Add-Printer -ConnectionName "\\\\printsrv01\\ERL-4OG-Farbe"
{{/code}}
"""

RICH_BACKUP_INST = u"""= Installation =

**Duplikat-Testfall 2 von 2.** Gleicher Titel wie
##Clientthemen.Drucker.Installation##, anderer Inhalt und anderer Pfad.

== Backup-Agent ausrollen ==

{{code language="bash"}}
./install-agent.sh --server backup01.example.com --schedule nightly
{{/code}}

{{info}}
Beide "Installation"-Seiten stehen mit Status **Migrieren** in der Eingabeliste.
{{/info}}
"""

RICH_MONITORING = u"""= Monitoring Übersicht =

Diese Seite steht **nicht** in den Eingabe-Excel-Dateien und dient als Testfall
fuer unbekannte Seiten im Report.

|= System |= Check |= Intervall
| backup01 | Dienststatus | 5 min
| wsus01 | Sync-Status | 1 h
| pts-lab | Ping | 1 min
"""

RICH_HIDDEN = u"""= Persönliche Notizen (versteckte Seite) =

Diese Seite ist in xWiki als **hidden** markiert. Laut Anforderungen muessen
versteckte Benutzerseiten **trotzdem migriert** werden.

* Testfall: hidden = true
* Erwartung: Seite erscheint in Export und Report
"""

FOLDER_CLIENT = u"""= Clientthemen =

Sammelseite ("Überordner") fuer alle Client-Themen. In Confluence soll daraus
eine **normale Seite** werden, unter der die Unterseiten haengen.

== Inhalt ==

* PTS Protokolltestsystem
* Hyper-V unter Accenture
* Mainboard-Tausch
* Windows (Unterordner)
* VMWare Client (Unterordner)
* Drucker (Unterordner)
"""

FOLDER_SERVER = u"""= Serverthemen =

Sammelseite ("Überordner") fuer Serverbetrieb.

* Toolbox Installationsanleitung
* Wsus (Unterordner)
* Backup (Unterordner)
* Monitoring
"""


# --------------------------------------------------------------------------
# The test data set
# --------------------------------------------------------------------------
# Each entry: (spaces, page, title, content, tags, comments, attachments, hidden,
#              extra_revisions)
# extra_revisions produces real xWiki history so that "last editor" differs from
# the original author.

def build_pages():
    pts_img = make_png("PTS Oberflaeche - Testbild", (0, 120, 150))
    mb_img = make_png("Mainboard Hinweis - Testbild", (150, 60, 0))

    return [
        # ---- Clientthemen -------------------------------------------------
        (["Clientthemen"], "WebHome", u"Clientthemen", FOLDER_CLIENT,
         ["uebersicht", "client"], [], [], False, 1),

        (["Clientthemen", "PTS Protokolltestsystem auf Techniker Client"], "WebHome",
         u"PTS Protokolltestsystem auf Techniker Client", RICH_PTS,
         ["pts", "client", "protokolltest", "rollout-2025"],
         [u"Bitte vor dem Rollout die Portfreigabe pruefen. -- A. Mueller",
          u"Kompatibilitaetsmatrix auf 4.8.2 aktualisiert."],
         [("pts-oberflaeche.png", pts_img), ("pts-testlauf.log", LOG_ASSET)], False, 3),

        (["Clientthemen", "Hyper-V Host unter Accenture"], "WebHome",
         u"Hyper-V Host unter Accenture Clients", RICH_HYPERV,
         ["hyper-v", "virtualisierung", "accenture"],
         [u"Freigabeprozess seit Q3/2025 ueber das neue Ticketformular."],
         [], False, 2),

        (["Clientthemen", "Mainboard Tausch"], "WebHome",
         u"Mainboard Tausch - Rechnerwiederinbetriebnahme", RICH_MAINBOARD,
         ["hardware", "bitlocker", "tpm"],
         [u"TPM muss vor dem ersten Boot geleert werden, sonst schlaegt die Bindung fehl."],
         [("mainboard-hinweis.png", mb_img), ("geraeteliste.csv", CSV_ASSET)], False, 4),

        (["Clientthemen", "Taschenrechner-App reparieren Windows 10"], "WebHome",
         u"Taschenrechner-App reparieren Windows 10", RICH_TASCHENRECHNER,
         ["windows-10", "veraltet"], [], [], False, 2),

        (["Clientthemen", "Energieoptionen beim Betrieb mit Stromversorgung"], "WebHome",
         u"Energieoptionen beim Betrieb mit Stromversorgung", RICH_ENERGIE,
         ["energie", "veraltet"], [], [], False, 1),

        (["Clientthemen", "Notebook Uebergabe"], "WebHome",
         u"Notebook-Übergabe & -Rückgabe (Größe/Prüfung)", RICH_UMLAUT,
         ["hardware", "prozess", "sonderzeichen"],
         [u"Protokoll bitte im Ticket ablegen - äöü Test."], [], False, 1),

        # Unterordner Windows
        (["Clientthemen", "Windows"], "WebHome", u"Windows", FOLDER_CLIENT,
         ["windows", "uebersicht"], [], [], False, 1),
        (["Clientthemen", "Windows", "aktuelle Windows 10 Enterprise Iso downloaden"],
         "WebHome", u"aktuelle Windows 10 Enterprise Iso downloaden", RICH_ISO,
         ["windows-10", "iso", "veraltet"],
         [u"VLSC wurde durch das Microsoft 365 Admin Center abgeloest."], [], False, 3),

        # Unterordner VMWare Client
        (["Clientthemen", "VMWare Client"], "WebHome", u"VMWare Client",
         u"= VMWare Client =\n\nÜberordner fuer VMWare-Themen auf Clients.",
         ["vmware", "uebersicht"], [], [], False, 1),
        (["Clientthemen", "VMWare Client", "VMWare Side Channel Mitigation"], "WebHome",
         u"VMWare Side Channel Mitigation", RICH_VMWARE,
         ["vmware", "security", "performance"],
         [u"Freigabe durch Security liegt fuer das Labor Erlangen vor."], [], False, 2),

        # Unterordner Drucker (Duplikat-Titel)
        (["Clientthemen", "Drucker"], "WebHome", u"Drucker",
         u"= Drucker =\n\nÜberordner fuer Druckerthemen.", ["drucker"], [], [], False, 1),
        (["Clientthemen", "Drucker"], "Installation", u"Installation", RICH_DRUCKER_INST,
         ["drucker", "installation", "duplikat-test"], [], [], False, 1),

        # ---- Serverthemen -------------------------------------------------
        (["Serverthemen"], "WebHome", u"Serverthemen", FOLDER_SERVER,
         ["uebersicht", "server"], [], [], False, 1),

        (["Serverthemen", "Toolbox Installationsanleitung"], "WebHome",
         u"Toolbox Installationsanleitung", RICH_TOOLBOX,
         ["toolbox", "installation", "server"],
         [u"Silent-Schalter fuer die Minimalvariante ergaenzt."], [], False, 2),

        (["Serverthemen", "Wsus"], "WebHome", u"Wsus",
         u"= Wsus =\n\nÜberordner fuer WSUS-Betriebsthemen.", ["wsus", "uebersicht"],
         [], [], False, 1),
        # Terminal page (kein WebHome) - so wie in der Excel-Zeile ohne /WebHome
        (["Serverthemen", "Wsus"], "Server verschwindet aus WSUS",
         u"Server verschwindet aus WSUS", RICH_WSUS_MISSING,
         ["wsus", "troubleshooting"],
         [u"Betrifft nur geklonte Systeme aus dem Template W2019-Base."], [], False, 3),
        (["Serverthemen", "Wsus", "Patchen der Windows Server mittels Wsus"], "WebHome",
         u"Patchen der Windows Server mittels Wsus", RICH_WSUS_PATCH,
         ["wsus", "patchmanagement", "betrieb"],
         [u"Ring 3 wurde auf Samstag 22:00 verschoben.",
          u"Bitte Snapshots vor dem Patchen pruefen."],
         [("patchlauf.log", LOG_ASSET)], False, 4),

        (["Serverthemen", "Backup"], "WebHome", u"Backup",
         u"= Backup =\n\nÜberordner fuer Sicherungsthemen.", ["backup"], [], [], False, 1),
        (["Serverthemen", "Backup"], "Installation", u"Installation", RICH_BACKUP_INST,
         ["backup", "installation", "duplikat-test"], [], [], False, 1),

        (["Serverthemen", "Monitoring"], "WebHome", u"Monitoring Übersicht",
         RICH_MONITORING, ["monitoring"], [], [], False, 1),

        # ---- Versteckte Benutzerseite -------------------------------------
        (["PersoenlicheNotizen"], "WebHome", u"Persönliche Notizen",
         u"= Persönliche Notizen =\n\nBenutzerbereich mit versteckten Seiten.",
         ["persoenlich"], [], [], True, 1),
        (["PersoenlicheNotizen"], "VersteckteSeite", u"Persönliche Notizen (versteckt)",
         RICH_HIDDEN, ["persoenlich", "hidden-test"],
         [u"Versteckte Seiten muessen laut Anforderung mitgenommen werden."],
         [], True, 2),
    ]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--wipe", action="store_true",
                        help="delete previously seeded spaces before creating")
    args = parser.parse_args()

    try:
        probe = session.get(XWIKI_URL + "/rest/wikis/xwiki/spaces",
                            headers={"Accept": "application/json"}, timeout=10)
        probe.raise_for_status()
    except Exception as exc:
        print("xWiki nicht erreichbar unter {}: {}".format(XWIKI_URL, exc))
        return 1

    if args.wipe:
        wipe()

    create_users()

    print("Creating test pages ...")
    for (spaces, page, title, content, tags, comments, attachments,
         hidden, revisions) in build_pages():
        author = author_for(spaces)
        author_sess = user_session(author)
        editor_sess = user_session(editor_for(author))

        if not put_page(spaces, page, title, content, hidden=hidden, sess=author_sess):
            continue

        # Later revisions are written by someone else, so the last editor
        # differs from the creator.
        for i in range(2, revisions + 1):
            put_page(spaces, page, title,
                     content + u"\n\n(% style=\"color:#666\" %)//Ueberarbeitung {} - "
                               u"Inhalt geprueft.//(%%)".format(i),
                     hidden=hidden, sess=editor_sess)

        if tags:
            set_tags(spaces, page, tags, sess=editor_sess)
        for c in comments:
            add_comment(spaces, page, c, sess=editor_sess)
        for name, data in attachments:
            add_attachment(spaces, page, name, data, sess=editor_sess)

    print("\nFertig. Naechster Schritt:")
    print("  go run . --mode export")
    return 0


if __name__ == "__main__":
    sys.exit(main())
