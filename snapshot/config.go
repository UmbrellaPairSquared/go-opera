package snapshot

type Config struct {
  Save bool
  SnapshotPath string
}

func DefaultConfig() (Config) {
  return Config{
    Save: false,
    SnapshotPath: "",
  }
}
