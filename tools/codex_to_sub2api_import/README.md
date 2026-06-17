# Codex JSON to sub2api Import

Convert one or more Codex/OpenAI OAuth JSON files into the sub2api account data import format.

## Visual UI

Open this file in your browser:

```text
tools/codex_to_sub2api_import/index.html
```

Select or drag multiple JSON files, review the generated accounts, then download the merged import JSON.
For a directory such as `C:\Users\Administrator\Desktop\bugteam`, use the "选择文件夹" button.

## Usage

```powershell
python .\tools\codex_to_sub2api_import\convert.py `
  "D:\path\to\*.json" `
  -o ".\output\sub2api-import.json"
```

The default output is a full sub2api data import payload:

```json
{
  "type": "sub2api-data",
  "version": 1,
  "exported_at": "2026-06-09T00:00:00Z",
  "proxies": [],
  "accounts": []
}
```

Import it from the admin account data import dialog. Multiple input JSON files are merged into one `accounts` array.

## Options

- `--accounts-only`: output only the account array for the batch-create importer.
- `--concurrency 3`: set account concurrency.
- `--priority 50`: set account priority.
- `--name-prefix "prefix-"`: prefix generated account names.
- `--notes "text"`: add the same notes to every account.
- `--dedupe email|file|none`: deduplicate by email by default.

The generated file contains OAuth tokens. Keep it private and delete temporary copies after import.
