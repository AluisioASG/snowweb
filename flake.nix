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
        devShell = final.mkShell {
          buildInputs = with final; [ go golangci-lint gopls reuse ];
        };
      };
    };
  };
}
