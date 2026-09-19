{
  description = "Sub-millisecond keyboard layout sync between host and guest Sway instances";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        rec {
          sway-layout-sync = pkgs.buildGoModule {
            pname = "sway-layout-sync";
            version = "0.0.1";

            src = ./.;

            vendorHash = null;
          };

          default = sway-layout-sync;
        }
      );

      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = [ pkgs.go ];
          };
        }
      );
    };
}
