package buildinfo

// Version is set at build time via -ldflags, and stays "dev" for builds that skip the Makefile.
var Version = "dev"
