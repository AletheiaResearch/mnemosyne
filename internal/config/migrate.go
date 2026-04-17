package config

func Migrate(cfg Config) Config {
	cfg.normalize()
	return cfg
}
