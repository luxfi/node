{
  # To use:
  #  - install nix: `./scripts/run_task.sh install-nix`
  #  - run `nix develop` or use direnv (https://direnv.net/)
  #    - for quieter direnv output, set `export DIRENV_LOG_FORMAT=`

  description = "Lux Node development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/flake-compat";
  };

  outputs = { self, nixpkgs }:
    let
      systems = ["x86_64-linux" "aarch64-linux"];
      mkShells = f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in
    {
      devShells = mkShells (pkgs: {
        default = pkgs.mkShell {
          buildInputs = with pkgs; [ git go-task buf shellcheck protoc-go protoc-go-grpc protoc-gen-connect-go solc ];
        };
      });
    };
}
