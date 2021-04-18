# SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
#
# SPDX-License-Identifier: MIT

{
  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };
  outputs = { self, flake-utils, nixpkgs }: flake-utils.lib.simpleFlake rec {
    inherit self nixpkgs;
    name = "snowweb";
    overlay = final: prev: {
      ${name} = {
        defaultPackage = final.buildGoModule {
          inherit name;
          src = self;
          vendorSha256 = "sha256-6S6OgkSf6xiEHbxRfgDZD/rRlfTn5ryG1evIxNCxwhs=";
          meta = with nixpkgs.lib; {
            description = "Static website server for Nix packages";
            homepage = "https://git.sr.ht/~aasg/snowweb";
            license = licenses.agpl3;
            maintainers = with maintainers; [ AluisioASG ];
            platforms = platforms.linux;
          };
        };

        devShell = final.mkShell {
          buildInputs = with final; [ go golangci-lint gopls reuse ];
        };
      };
    };
  };
}
