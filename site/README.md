# Airframe marketing site

This is a buildless landing page. Serve this directory with any static host, or run this from the repository root for local review:

```sh
python3 -m http.server --bind 127.0.0.1 --directory site 8000
```

Open [the local preview](http://127.0.0.1:8000/). The copy button uses the browser clipboard on HTTPS or loopback. The page and command examples also work as static HTML.

## Brand assets

| Asset | Use |
| --- | --- |
| [airframe-logo.svg](assets/airframe-logo.svg) | Horizontal lockup for dark surfaces. |
| [airframe-logo-light.svg](assets/airframe-logo-light.svg) | Horizontal lockup for light surfaces. |
| [airframe-mark.svg](assets/airframe-mark.svg) | Standalone mark with a transparent background, for dark surfaces. |
| [airframe-icon.svg](assets/airframe-icon.svg) | Scalable application icon on a midnight tile. |
| [airframe-icon-1024.png](assets/airframe-icon-1024.png) | Full-resolution application icon export. |
| [airframe-icon-180.png](assets/airframe-icon-180.png) | Apple touch icon. |
| [favicon.svg](assets/favicon.svg) | Simplified browser icon for very small sizes. |

The three ribs form an A and represent the primitive, semantic, and component tiers from base to top. The corner brackets, midnight `#101E30`, mist `#F5F7F8`, Inter, and JetBrains Mono come from Starboard’s visual vocabulary. Airframe uses C65’s ember `#DD8554` as its signature color and uses Starboard’s mint `#68DBC6` for terminal feedback. The light lockup uses darker ember `#A34C28`.

Product links go to the Airframe repository and its releases. The repository is public, so visitors can view the source and download releases without signing in.
