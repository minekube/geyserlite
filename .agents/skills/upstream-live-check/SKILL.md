---
name: upstream-live-check
description: Check live Minecraft, Geyser, Gate, and geyserlite release and CI state with gh before making version-support, preview, or release claims.
---

# Upstream live check

Do not rely on cached knowledge for Minecraft, Geyser, Gate, Bedrock protocol
support, releases, or CI status (see `AGENTS.md`). Run these checks first.

## Useful checks

```sh
gh repo view minekube/geyserlite --json defaultBranchRef,homepageUrl,url
gh release view --repo minekube/geyserlite --json tagName,publishedAt,url,assets
gh api repos/GeyserMC/Geyser/commits/master --jq '{sha:.sha,date:.commit.committer.date,message:.commit.message}'
VERSION="the-version-you-are-investigating"
gh pr list --repo GeyserMC/Geyser --state open --search "$VERSION OR protocol OR Minecraft" --json number,title,url,isDraft,updatedAt
```

## Support claims

For user-facing support claims, also check:

- Mojang/Minecraft release notes for the Java and Bedrock versions involved.
- GeyserMC supported versions and relevant Geyser pull requests or releases.
- Current Minekube Gate and geyserlite release state if a managed update chain
  is involved.

## When a backend outruns Geyser support

When a backend upgrades to a brand-new Java server version before Geyser has
official support, the stable production recommendation is to wait. ViaLite
behind Gate may bridge the Java backend protocol for early adopters if Geyser
can still connect to Gate, but it cannot fix unsupported Bedrock protocols or
Geyser translation gaps.
