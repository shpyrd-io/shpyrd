---
title: Getting started
pageTitle: shpyrd - Getting started
description: Install shpyrd - with the installer, or manually - then ask your agent to put your app online and share it with your team.
---

You built an app with your agent. shpyrd is where it goes next: its own address, a sign-in in front of it, and the colleagues you choose inside. You don't learn a new tool to get there - you add shpyrd to the agent you already use, and tell it what you want. {% .lead %}

## 1. Install shpyrd

### With the installer

Download it for your computer and open it. It installs the shpyrd CLI, adds shpyrd to every AI agent it finds on your computer, and offers to sign you in.

{% add-to-agent /%}

Or pick the download yourself:

- **macOS**: [Shpyrd-Installer.dmg](https://github.com/shpyrd-io/shpyrd/releases/download/installer-latest/Shpyrd-Installer.dmg)
- **Windows**: [Shpyrd-Installer.exe](https://github.com/shpyrd-io/shpyrd/releases/download/installer-latest/Shpyrd-Installer.exe)
- **Linux**: [Shpyrd-Installer-linux-amd64.tar.gz](https://github.com/shpyrd-io/shpyrd/releases/download/installer-latest/Shpyrd-Installer-linux-amd64.tar.gz)

### Manually

The same three things the installer does.

**Add shpyrd to your agent.** shpyrd has a server for AI agents at `https://mcp.shpyrd.io/mcp`. It needs no account: it tells your agent how to build an app that runs on shpyrd - where it keeps its data, how it knows who is signed in - and where the documentation is. Pick the agent you use and add it once:

{% agent-setup /%}

**Install the shpyrd CLI.** It is how your agent puts apps online and shares them, as you. On macOS and Linux:

```shell
curl -fsSL https://shpyrd.io/install.sh | sh
# or, on macOS, with Homebrew
brew install shpyrd-io/tap/shpyrd
```

**Sign in.** New to shpyrd? Create your account and your free workspace on shpyrd cloud:

```shell
shpyrd login --signup
```

Your browser opens on the signup. Prove your email and name your workspace - it answers at its own address, like `acme.shpyrd.cloud`, and every app you put online gets an address of its own, like `acme-purchases.shpyrd.app`. The CLI is signed in to it when it is ready.

Already have a workspace? Sign in to it:

```shell
shpyrd login --url https://acme.shpyrd.cloud
```

Your browser opens on its sign-in. Approve the code your terminal shows. Plain `shpyrd login` asks which of the two you want. The CLI is signed in as you for 30 days, and never with more than your roles allow.

## 2. Talk to it

In the folder of your app, say what you want, the way you'd say it to a colleague:

{% chat detail="~/projects/purchases" %}
{% message from="person" %}
Put this app online on shpyrd.
{% /message %}
{% message from="agent" steps=["Deployed purchase-requests · release 1", "acme-purchases.shpyrd.app is live · sign-in required"] %}
Done. It's at **acme-purchases.shpyrd.app**, behind your workspace's sign-in. Nobody else can open it yet.
{% /message %}
{% message from="person" %}
Share it with the Finance team, and let Ana change it too.
{% /message %}
{% message from="agent" steps=["Finance · can use", "ana@acme.com · can update"] %}
Done. Everyone in Finance can open it - and whoever joins Finance later. Ana can deploy updates and roll them back.
{% /message %}
{% /chat %}

And a week later:

{% chat detail="~/projects/purchases" %}
{% message from="person" %}
Finance says the totals are wrong since this morning.
{% /message %}
{% message from="agent" steps=["Read the logs of release 3: errors in /totals since 09:12", "Rolled back to release 2"] %}
Release 3 broke the totals, so everyone is on release 2 again - its code and its settings. Want me to look at the fix?
{% /message %}
{% /chat %}

The people you shared it with sign in with their account and find the app among theirs. [Sign-in for your app](/docs/app-access) and [Teams, roles and security](/docs/access) say who can do what.

{% callout title="How your agent does it" %}
Before it writes code, your agent reads from shpyrd's server how an app should be built to run there. To put it online, share it and roll it back, it runs the shpyrd CLI for you, signed in as you. To also let it read your projects, logs and metrics from anywhere, add your workspace's own server - [AI assistants (MCP)](/docs/mcp) says how.
{% /callout %}

## What you get

- **An address and a sign-in for every app.** People sign in before a request reaches your app, and the app is told who they are - no login code of its own.
- **Access by team or by person.** Your company's sign-in, its groups as teams, and separate rights to use an app, to change it and to manage it.
- **Changes you can take back.** Every deploy or settings change is a numbered release; going back restores the code and the settings together.
- **What apps need to run.** Databases and caches, settings that stay secret, logs and metrics - and apps that sleep when nobody uses them and wake on the next visit.

## Prefer commands?

Everything your agent does is an ordinary command you can type yourself: [Deploying](/docs/deploying) walks through them, and the [CLI reference](/docs/cli) lists them all.

## Running it yourself

shpyrd is open source (MPL-2.0) and runs on a Kubernetes cluster of your own too - on your laptop, on [AWS](/docs/aws) or on [Oracle Cloud](/docs/oracle-cloud). [Installation](/docs/installation) has every step.

## Status

shpyrd is in beta and developed in the open. The [roadmap](/docs/roadmap) shows what is done and what comes next; [how to contribute](/docs/how-to-contribute) explains the way in.

## Getting help

- Bugs, ideas and questions: [GitHub issues](https://github.com/shpyrd-io/shpyrd/issues).
- Community chat: [Discord](https://discord.gg/RYAT4wNKfw).
