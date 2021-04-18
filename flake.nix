# SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
#
# SPDX-License-Identifier: MIT

{

  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, flake-utils, nixpkgs }:
    let

      exports = {
        overlay = final: prev: {
          "snowweb" = {
            defaultPackage = final.buildGoModule {
              name = "snowweb";
              src = self;
              vendorSha256 = "sha256-6gV0/m0Ygxs5LM9biNJtGMaKDkyEBH88rkPtg0tRJus=";
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

        nixosModule = { config, lib, pkgs, ... }:
          let
            inherit (lib) flip mapAttrs' mkEnableOption mkIf mkOption optionalString types;

            siteOpts = {
              options = {
                installable = mkOption {
                  description = "Package to serve, given as an argument to `nix build`.";
                  type = types.str;
                };

                listenAddress = mkOption {
                  description = ''
                    Network address at which the web server will listen, given as a colon-separated pair of type and address.
                  '';
                  type = types.str;
                  default = "tcp:127.0.0.1:0";
                };

                certFile = mkOption {
                  description = "Path to the PEM-encoded certificate used for HTTPS.";
                  type = types.nullOr types.path;
                  default = null;
                };

                keyFile = mkOption {
                  description = "Path to the PEM-encoded key used for HTTPS.";
                  type = types.nullOr types.path;
                  default = null;
                };

                clientCaFile = mkOption {
                  description = ''
                    Path to the PEM-encoded certificate bundle used for client verification.
                    This is required to enable the remote control API.
                  '';
                  type = types.nullOr types.path;
                  default = null;
                };
              };
            };

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
                type = types.attrsOf (types.submodule siteOpts);
                default = { };
              };
            };

            config = mkIf cfg.enable {
              systemd.services = flip mapAttrs' cfg.sites (siteName: siteCfg: {
                name = "snowweb@${siteName}";
                value = {
                  description = "SnowWeb web server for ${siteName}";
                  requires = [ "network.target" ];
                  after = [ "network.target" ];
                  wantedBy = [ "multi-user.target" ];
                  environment.NIX_REMOTE = "daemon";
                  environment.XDG_CACHE_HOME = "/tmp";
                  path = [ cfg.nixPackage pkgs.gitMinimal ];
                  serviceConfig = {
                    Type = "exec";
                    ExecStart = ''
                      ${cfg.package}/bin/snowweb \
                      ${optionalString (siteCfg.certFile != null) "-certificate ${siteCfg.certFile}"} \
                      ${optionalString (siteCfg.keyFile != null) "-key ${siteCfg.keyFile}"} \
                      ${optionalString (siteCfg.clientCaFile != null) "-client-ca ${siteCfg.clientCaFile}"} \
                      -listen ${siteCfg.listenAddress} \
                      -log journald \
                      ${siteCfg.installable}
                    '';
                    ExecReload = "${pkgs.coreutils}/bin/kill -HUP $MAINPID";
                    Restart = "on-failure";

                    DynamicUser = true;
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
