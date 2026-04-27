## agenc mission search

Search missions by conversation content

### Synopsis

Search missions using full-text search over conversation transcripts.

Searches user messages, assistant responses, session titles, and mission prompts.
Results are ranked by relevance using BM25.

The search index is populated automatically by the server. New content becomes
searchable within ~30 seconds of being written.

```
agenc mission search <query> [flags]
```

### Options

```
  -h, --help        help for search
      --json        output results as JSON
      --limit int   maximum number of results (default 20)
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

