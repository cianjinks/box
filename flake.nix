{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      flake-utils,
      nixpkgs,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShell = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.delve
            pkgs.gopls
            # goimports, etc
            pkgs.gotools
            # container
            pkgs.umoci
          ];
        };
      }
    );
}
