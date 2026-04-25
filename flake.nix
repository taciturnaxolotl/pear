{
  description = "pear — strip any recipe URL down to what matters";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs =
    {
      self,
      nixpkgs,
    }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
        "x86_64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system: {
        default =
          let
            pkgs = nixpkgs.legacyPackages.${system};
          in
          pkgs.buildGoModule {
            pname = "pear";
            version = "0.1.0";

            src = ./.;

            vendorHash = "sha256-qnvBWpHLZZq0R8QEhDJeclVlHEbnru6v2RkPnKIGMAY=";

            ldflags = [
              "-X main.gitHash=${self.rev or self.dirtyRev or "dev"}"
            ];

            meta = with pkgs.lib; {
              description = "Nice recipes — strip any recipe URL down to what matters";
              homepage = "https://pear.dunkirk.sh";
              license = licenses.mit;
              platforms = platforms.unix;
            };
          };
      });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/pear";
        };
      });

      devShells = forAllSystems (system: {
        default =
          let
            pkgs = nixpkgs.legacyPackages.${system};
          in
          pkgs.mkShell {
            packages = with pkgs; [
              go
              sqlite
            ];

            shellHook = ''
              export DATABASE_URL="pear.db"
            '';
          };
      });
    };
}