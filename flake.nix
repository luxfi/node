{
  description = "luxd — the node, and the toolchain it is built with";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      eachSystem = f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));

      # go.mod declares 1.26.8 and nothing else will do: a shell that hands the
      # build 1.26.7 sends `go` off to fetch 1.26.8 at first use, which is the
      # download this shell exists to remove. nixpkgs tracks its own schedule,
      # so the version is taken from upstream's own release by hash.
      #
      # The BINARY release, not the source: building the toolchain costs an hour
      # of a machine that has production on it, and buys nothing here — the
      # tarball is hash-pinned either way, so the shell is reproducible and the
      # first entry is a download rather than a compile.
      goVersion = "1.26.8";
      goHash = {
        x86_64-linux = "sha256-0PdDsz6NiUXmsfQy7dFXhccFBxIdbipyOyEoXt34tXs=";
        aarch64-linux = "sha256-IR/87Z3LljOlXqxjZIFuwN3ZUTiadA6I+oszN5cb3aA=";
      };
      goArch = { x86_64-linux = "amd64"; aarch64-linux = "arm64"; };

      go = pkgs:
        let s = pkgs.stdenv.hostPlatform.system; in
        pkgs.stdenv.mkDerivation {
          pname = "go";
          version = goVersion;
          src = pkgs.fetchurl {
            url = "https://go.dev/dl/go${goVersion}.linux-${goArch.${s}}.tar.gz";
            hash = goHash.${s};
          };
          # The distribution is already built; patchelf is the only work.
          nativeBuildInputs = [ pkgs.autoPatchelfHook ];
          buildInputs = [ pkgs.stdenv.cc.cc.lib ];
          installPhase = "mkdir -p $out && cp -r . $out/";
          # Go ships test data with deliberately broken ELF headers.
          dontStrip = true;
          autoPatchelfIgnoreMissingDeps = true;
          meta.mainProgram = "go";
        };
    in {
      devShells = eachSystem (pkgs: {
        default = pkgs.mkShell {
          packages = [
            (go pkgs)
            pkgs.gcc          # CGO: cevm links a C++ EVM
            pkgs.pkg-config
            pkgs.git
          ];
          # GOTOOLCHAIN=local forbids the silent download this shell replaces:
          # if the pinned Go is ever wrong for go.mod, that is an error to see,
          # not a fetch to wait for.
          shellHook = ''
            export GOTOOLCHAIN=local
            export GOFLAGS=-mod=readonly
          '';
        };
      });
    };
}
