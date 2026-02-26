# config — INI config parsing, hook toggles, and sink configuration

All files stable — prefer this summary over reading source files.

## config.go

```go
var AllHookTypes = []string{...} // 15 canonical hook types in display order

type HookEntry struct { Name string; Enabled bool }
type HookConfig struct { Hooks []HookEntry }

func ReadConfig(path string) (HookConfig, error)
func WriteConfig(path string, cfg HookConfig) error
func IsEnabled(path, hookName string) bool

type SinkConfig struct { Forward bool; Endpoint string }
func ReadSinkConfig(path string) SinkConfig

type CacheConfig struct { Enabled bool }
func ReadCacheConfig(path string) CacheConfig

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error
```

INI parser with BOM stripping, case-insensitive keys, inline comment stripping, last-wins for duplicates. WriteConfig preserves non-[hooks] sections. AtomicWriteFile uses temp+rename pattern. ReadSinkConfig reads [sink] section (default endpoint: localhost:9800/ingest). ReadCacheConfig reads [cache] section (default: enabled, fail-open).

## config_test.go, config_edge_test.go

Tests: ReadConfig, WriteConfig, IsEnabled, ReadSinkConfig, AtomicWriteFile, concurrent read/write safety. Edge cases: BOM, Windows line endings, multiple sections, inline comments.

No concurrency primitives. No internal imports.
