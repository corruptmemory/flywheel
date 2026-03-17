# Portability and CI/CD Research — 2026-03-16

## Context

Two questions for future planning: (1) Can flywheel's jail isolation work on non-Arch Linux distros? (2) How would flywheel plug into automated build pipelines?

## Cross-Distro Linux Portability

### Already Portable

- **Go code** — stdlib + go-flags, nothing distro-specific
- **`unshare`, `mount`, `chroot`, `runuser`** — all from util-linux, present on every distro
- **`/proc`, `/sys`, `/dev`** — kernel-level, distro-agnostic
- **Claude Code** at `/opt/claude-code/` — installs the same way everywhere
- **Merged-usr vs traditional layout** — jail package already detects symlinks vs real directories for `/bin`, `/lib`, `/lib64`, `/sbin` (Arch/Fedora merged-usr vs older Debian/Ubuntu traditional)

### Needs Attention

| Concern | Details | Fix |
|---------|---------|-----|
| `/etc/subuid` and `/etc/subgid` | Must exist with entries for the user. Some distros don't configure by default. Without them, `--map-auto` fails. | Detect and error with helpful message ("run `usermod --add-subuids ...`") |
| `unprivileged_userns_clone` | Some distros (older Debian/Ubuntu) disable user namespaces by default. | `CheckUnsharePreflight()` already checks this. Fixable: `sysctl kernel.unprivileged_userns_clone=1` |
| **AppArmor** (Ubuntu/Debian) | May block namespace operations or mounts inside user namespaces. | Needs testing. Likely needs a profile exception or `aa-complain` mode. |
| **SELinux** (RHEL/Fedora) | May block similar operations. | Needs testing. May need `setenforce 0` for testing or a custom policy module. |
| tmux version | `new-session` with command argument works everywhere recent. Very old versions might differ. | Unlikely problem on anything from the last 5 years. |
| `chmod 777` workaround | Ugly. The proper fix: read subuid offset from `/etc/subuid`, compute the mapped UID, `chown` to it. | Should be done before leaving developer machines. |

### CI/Container Environments (Hardest Target)

Running inside a container (GitHub Actions, GitLab CI runners, Docker) is the most challenging:
- User namespaces often disabled
- Mount capabilities restricted
- Nested containers require `--privileged` or `SYS_ADMIN` capability
- Some CI providers have security policies that block all of this

**Workarounds:**
1. Fall back to `RootJail` when user namespaces aren't available (CI runners often have root)
2. Use `--privileged` Docker containers for the CI job
3. Run the entire flywheel pipeline on a dedicated VM (simplest, most reliable)

### Recommended Path

1. Extend `CheckUnsharePreflight()` to check for: subuid config, AppArmor/SELinux profiles, container detection (`/.dockerenv`, cgroup check)
2. Provide clear error messages with fix instructions per distro
3. Test on Ubuntu 22.04/24.04 (most common CI base), Fedora, Debian
4. Fall back to RootJail when UnshareJail prerequisites aren't met
5. Replace `chmod 777` with proper `chown` using computed subuid offset

## CI/CD Integration Research

### Philosophy

Jim's position (strongly held): traditional forge CI pipelines (GitHub Actions, GitLab CI) are YAML-driven vendor lock-in with limited upside. The preferred approach: a custom runner that executes scripts directly. Flywheel's pipeline is already a self-contained Go binary — it doesn't need YAML orchestration on top.

### Gitea Actions

Researched as a potential self-hosted forge: https://about.gitea.com/

**What it is:** Gitea is a self-hosted Git forge (like GitHub/GitLab but lightweight). Gitea Actions is its built-in CI/CD, available since v1.19, production-stable.

**Runner: `act_runner`**
- Standalone Go binary (forked from `nektos/act`)
- Self-hosted, registers against your Gitea instance
- Supports three execution modes:
  1. **Docker containers** (default) — each job runs in a container
  2. **Host mode** — jobs run directly on the host machine, no Docker
  3. **Kubernetes** — auto-scaling runners (enterprise)

**Host mode is the key feature:**
```bash
# Register runner with host execution (no Docker needed)
./act_runner register --no-interactive \
  --instance https://gitea.example.com \
  --token <token> \
  --labels "flywheel:host"
```

With `flywheel:host` label, jobs execute directly on the runner machine. The workflow YAML becomes minimal:

```yaml
name: Flywheel Build
on: [push]
jobs:
  build:
    runs-on: flywheel
    steps:
      - run: |
          cd ~/projects/flywheel
          ./flywheel --builder-dir ... --uat-dir ... --skip-permissions --model opus
```

That's it. The YAML is just a trigger — the actual work is the flywheel binary running scripts as intended.

**Compatibility:** ~GitHub Actions compatible. Can reuse actions like `actions/checkout@v4`. But for flywheel's use case, we don't need any of that — just `run: ./flywheel`.

**The appeal for flywheel:**
- Self-hosted (runs on your machines, your jails, your rules)
- Host mode means no Docker-in-Docker nonsense
- The runner is a Go binary — same ecosystem
- Minimal YAML (just a trigger, not an orchestration language)
- Git forge included — could replace GitHub for the builder workspaces
- Lightweight — runs on a Raspberry Pi if you want

**What Gitea does NOT solve:**
- The jail isolation still needs Linux kernel features (user namespaces)
- The runner machine needs all the same prerequisites as running flywheel locally
- No magic — it's just a trigger mechanism that runs your binary on a registered machine

### Other Options Considered

| Option | Verdict |
|--------|---------|
| **GitHub Actions** | Custom self-hosted runner possible, but the YAML overhead and GitHub dependency are the problem flywheel is trying to avoid |
| **GitLab CI** | Same YAML issue. Self-hosted runners available but heavier infra. |
| **Gitea Actions** | Best fit — lightweight, self-hosted, host mode, minimal YAML |
| **Bare cron job** | Simplest. `crontab -e` + a shell script that pulls and runs flywheel. No forge integration, no webhook triggers. |
| **Webhook + systemd** | Gitea/GitHub webhook hits an endpoint, systemd unit runs flywheel. DIY but no YAML. |

### Recommendation

For now: run flywheel manually or via a simple shell script. For future automation: Gitea + `act_runner` in host mode. The YAML is one `run:` step — just a trigger, not an orchestration language. The actual pipeline logic stays in the Go binary where it belongs.

## References

- Gitea: https://about.gitea.com/
- Gitea Actions docs: https://docs.gitea.com/usage/actions/overview
- act_runner docs: https://docs.gitea.com/usage/actions/act-runner
