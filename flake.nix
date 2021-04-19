# SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
#
# SPDX-License-Identifier: MIT

{

  inputs = {
    aasg-nixexprs.url = "git+https://git.sr.ht/~aasg/nixexprs";
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, aasg-nixexprs, flake-utils, nixpkgs }:
    let

      exports = {
        overlay = final: prev: {
          "snowweb" = {
            defaultPackage = final.buildGoModule {
              name = "snowweb";
              src = self;
              vendorSha256 = "sha256-ZMDk5etjtpN2lEerumlcXmcTrh/PsgQKdnk8ZBhAReU=";
              meta = with nixpkgs.lib; {
                description = "Static website server for Nix packages";
                homepage = "https://git.sr.ht/~aasg/snowweb";
                license = licenses.agpl3;
                maintainers = with maintainers; [ AluisioASG ];
                platforms = platforms.linux;
              };
            };

            devShell = final.mkShell {
              buildInputs = with final; [ delve go golangci-lint gopls reuse ];
            };
          };
        };

        nixosModule = { config, lib, pkgs, ... }:
          let
            inherit (lib) flip mkEnableOption mkIf mkOption types;
            inherit (aasg-nixexprs.lib) concatMapAttrs updateNew;

            settingsToEnv =
              let
                inherit (lib) concatStringsSep flatten isAttrs isList listToAttrs mapAttrsToList nameValuePair toUpper;
                settingToPair = prefix: name: value:
                  let envName = toUpper "${prefix}_${name}";
                  in
                  if isAttrs value then
                    settingsToPairs envName value
                  else if isList value then
                    nameValuePair envName (concatStringsSep "," value)
                  else
                    nameValuePair envName (toString value);
                settingsToPairs = prefix: settings:
                  flatten (mapAttrsToList (settingToPair prefix) settings);
              in
              settings: listToAttrs (settingsToPairs "snowweb" settings);

            cfg = config.services.snowweb;
          in
          {
            options.services.snowweb = {
              enable = mkEnableOption "the SnowWeb static web server";

              package = mkOption {
                description = "snowweb package to use.";
                type = types.package;
                default = pkgs.snowweb.defaultPackage;
                defaultText = "pkgs.snowweb.defaultPackage";
              };

              nixPackage = mkOption {
                description = "nix package used to build sites.";
                type = types.package;
                default = config.nix.package;
                defaultText = "config.nix.package";
              };

              sites = mkOption {
                description = "Sites to serve through SnowWeb.";
                type = types.attrsOf (types.submodule {
                  freeformType = with types; let t = attrsOf (oneOf [ str (listOf str) t ]); in t;
                });
                default = { };
              };
            };

            config = mkIf cfg.enable {
              systemd.services = flip concatMapAttrs cfg.sites (siteName: siteCfg: {
                "snowweb@${siteName}" = {
                  description = "SnowWeb web server for ${siteName}";
                  requires = [ "network.target" ];
                  after = [ "network.target" ];
                  wantedBy = [ "multi-user.target" ];
                  environment = updateNew (settingsToEnv siteCfg) {
                    NIX_REMOTE = "daemon";
                    XDG_CACHE_HOME = "/tmp";
                  };
                  path = [ cfg.nixPackage pkgs.gitMinimal ];
                  serviceConfig = {
                    Type = "exec";
                    ExecStart = "${cfg.package}/bin/snowweb --tls-acme-storage=/var/lib/snowweb";
                    ExecReload = "${pkgs.coreutils}/bin/kill -HUP $MAINPID";
                    Restart = "on-failure";

                    DynamicUser = true;
                    StateDirectory = "snowweb";

                    NoNewPrivileges = true;
                    ProtectSystem = "strict";
                    ProtectHome = true;
                    ProtectKernelLogs = true;
                    ProtectKernelModules = true;
                    ProtectKernelTunables = true;
                    ProtectControlGroups = true;
                    PrivateDevices = true;
                    PrivateTmp = true;
                    DevicePolicy = "closed";
                    MemoryDenyWriteExecute = true;
                  };
                };
              });
            };
          };
      };

      outputs = flake-utils.lib.simpleFlake rec {
        inherit self nixpkgs;
        name = "snowweb";
        inherit (exports) overlay;
      };

    in

    exports // outputs;

}
