# xWiki to Confluence Migration Tool

A robust Go-based tool to migrate content from xWiki (v17.x+) to Confluence Cloud. 

## Features
- **2-Step Migration**: Decoupled Export (xWiki -> Local) and Import (Local -> Confluence).
- **Native Folders**: Supports Confluence Cloud's native folder system for hierarchical organization.
- **Metadata Support**: Migrates Tags (as Labels), Comments (with readable dates), and full Revision History.
- **Attachments**: Full support for images and other file attachments.

## Prerequisites
- **Go 1.21+** (if compiling from source).
- **xWiki Instance** with REST API enabled.
- **Confluence Cloud** account with API Token.

## Setup

1.  **Configuration**: Create a `.env` file in the project root (a template is provided).
2.  **Edit .env**: Fill in your xWiki and Confluence credentials:
    ```env
    XWIKI_URL=http://your-xwiki-host:8080
    XWIKI_USER=Admin
    XWIKI_PASSWORD=secret
    CONFLUENCE_USER=your-email@example.com
    CONFLUENCE_TOKEN=your-api-token
    ```
    *Flags will still override .env values if provided.*

## How to Use (Step-by-Step)

### Step 1: Export from xWiki
Fetch all content from xWiki and save it to a local data structure for inspection.
```bash
go run . --mode export --xwiki-url http://localhost:8080 --xwiki-user Admin --xwiki-password admin
```
*Creates an `./export` directory with JSON metadata and HTML content.*

### Step 2: Import to Confluence
Upload the locally stored data into your Confluence Cloud space.
```bash
go run . --mode import --confluence-url https://your-domain.atlassian.net/wiki --confluence-space-key YOURSPACE
```

## Offline Use (Portable Export)

To run the export on a machine without internet access:

1.  **Build the Tool**: On a machine WITH internet, run `build.bat` or:
    ```bash
    go build -mod=vendor -o migration.exe .
    ```
2.  **Transfer Files**: Copy the following to the offline machine:
    - `migration.exe`
    - `.env` (configured for your local xWiki)
3.  **Run Export**:
    ```bash
    ./migration.exe --mode export
    ```
    The tool will use the vendored dependencies included during the build and will not attempt to connect to any external services (except your local xWiki).

## Folder Detection Logic
The tool automatically converts xWiki pages to **Native Confluence Folders** if:
- The page name is `WebHome`.
- The page title contains the keyword *"Folder"* or *"Ordner"*.

Standard pages nested under these folders in xWiki will be correctly nested inside the corresponding Confluence folders.

## Command Line Flags
| Flag | Description | Default |
| :--- | :--- | :--- |
| `--mode` | `all`, `export`, or `import` | `all` |
| `--export-dir` | Path to store/read local data | `./export` |
| `--xwiki-url` | Base URL of xWiki | `http://localhost:8080` |
| `--confluence-space-key` | Target Confluence Key | `XWIKI` |

---
*Created as part of the Siemens xWiki-to-Confluence Migration project.*
