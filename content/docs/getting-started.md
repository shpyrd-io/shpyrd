---
title: Getting started
pageTitle: shpyrd - Getting started
description: Sign up for shpyrd cloud, add shpyrd to the agent you build with, and ask it to put your app online and share it with your team.
---

You built an app with your agent. shpyrd is where it goes next: its own address, a sign-in in front of it, and the colleagues you choose inside. You don't learn a new tool to get there - you add shpyrd to the agent you already use, and tell it what you want. {% .lead %}

## 1. Create your workspace

Sign up for shpyrd cloud and give your workspace a name. It answers at its own address, like `acme.shpyrd.app`, and every app you put online gets an address under it. There is nothing to install or run.

## 2. Add shpyrd to your agent

Your workspace is a connector for AI assistants, at your workspace's address followed by `/mcp` - like `https://acme.shpyrd.app/mcp`. Pick the agent you use and add it once:

{% agent-setup /%}

The first time, your agent sends you to your workspace to sign in and **Allow** it. It works as you, and never with more than your roles allow.

## 3. Talk to it

In the folder of your app, say what you want, the way you'd say it to a colleague:

{% chat title="Claude Code" detail="~/projects/purchases" %}
{% message from="person" %}
Put this app online on shpyrd.
{% /message %}
{% message from="agent" steps=["Deployed purchase-requests · release 1", "purchases.acme.shpyrd.app is live · sign-in required"] %}
Done. It's at **purchases.acme.shpyrd.app**, behind your workspace's sign-in. Nobody else can open it yet.
{% /message %}
{% message from="person" %}
Share it with the Finance team, and let Ana change it too.
{% /message %}
{% message from="agent" steps=["Finance · can use", "ana@acme.com · can update"] %}
Done. Everyone in Finance can open it - and whoever joins Finance later. Ana can deploy updates and roll them back.
{% /message %}
{% /chat %}

And a week later:

{% chat title="Claude Code" detail="~/projects/purchases" %}
{% message from="person" %}
Finance says the totals are wrong since this morning.
{% /message %}
{% message from="agent" steps=["Read the logs of release 3: errors in /totals since 09:12", "Rolled back to release 2"] %}
Release 3 broke the totals, so everyone is on release 2 again - its code and its settings. Want me to look at the fix?
{% /message %}
{% /chat %}

The people you shared it with sign in with their account and find the app among theirs. [Sign-in for your app](/docs/app-access) and [Teams, roles and security](/docs/access) say who can do what.

{% callout title="How your agent does it" %}
The connector answers your agent's questions about your projects today. To put an app online and change who can open it, your agent uses the shpyrd command line for you: the first time, it will ask to install it, and for a token from your workspace (**Workspace › API tokens**) to sign it in. After that, you only talk.
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
- Design changes go through short [RFCs](https://github.com/shpyrd-io/shpyrd/tree/main/rfcs).
- Community chat: [Discord](https://discord.gg/RYAT4wNKfw).
