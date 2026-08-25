# dbtools + MySQL — connection string reference

`url_env` for a MySQL target must resolve to a `mysql://`-scheme URL using
the driver's native host syntax:

```
mysql://user:pass@tcp(127.0.0.1:3306)/dbname
```

The `tcp(host:port)` form is not a standard URL host — that's expected and
required, not a typo. Don't try to "fix" it into `tcp://host:port` or
`host:port` alone; dbtools' MySQL engine parses it with the driver's own
`ParseDSN`, which expects this exact shape.

## What dbtools handles for you

- `multiStatements=true` is forced onto the URL automatically before
  running migrations, so a `.up.sql` file with multiple `;`-separated
  statements executes in full. You do not need to add this query param
  yourself.
- `parseTime=true` is applied for direct connections so `DATETIME`/
  `TIMESTAMP` columns scan as `time.Time`, not raw bytes.

## Red flag

Manually appending `multiStatements=true` or `parseTime=true` to your own
`url_env` value isn't wrong, but if you find yourself debugging a migration
that seems to only run its first statement, that's the exact silent-failure
symptom the forced param exists to prevent — check you're on a dbtools
version recent enough to have it (see `docs/roadmap.md`), rather than
assuming the DSN syntax itself is broken.
