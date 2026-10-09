#!/usr/bin/env python3
"""Convertit un compte rendu Markdown en PDF à la charte DecideOm.

Usage : python3 cr2pdf.py <fichier.md> [--client NomDuClient]
        (Windows : py cr2pdf.py atelier-01\\sorties\\CR-atelier-01.md)
        → produit <fichier>.pdf à côté du .md (les images en chemins relatifs sont incluses)

Nécessite pandoc et Google Chrome / Chromium / Microsoft Edge (tout en local, sans internet).
"""
import argparse
import datetime
import html
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

KIT = Path(__file__).resolve().parent
CHROMES = ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "msedge"]
# Emplacements habituels quand le navigateur n'est pas dans le PATH (Windows, macOS)
CHROME_PATHS = [
    r"%ProgramFiles%\Google\Chrome\Application\chrome.exe",
    r"%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe",
    r"%LocalAppData%\Google\Chrome\Application\chrome.exe",
    r"%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe",
    r"%ProgramFiles%\Microsoft\Edge\Application\msedge.exe",
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
]


def find_chrome():
    for name in CHROMES:
        if shutil.which(name):
            return shutil.which(name)
    for p in CHROME_PATHS:
        p = os.path.expandvars(p)
        if "%" not in p and Path(p).exists():
            return p
    return None


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("markdown")
    ap.add_argument("--client", default="")
    args = ap.parse_args()

    src = Path(args.markdown).resolve()
    if not src.exists():
        sys.exit(f"Introuvable : {src}")
    chrome = find_chrome()
    if not shutil.which("pandoc") or not chrome:
        sys.exit("Il faut pandoc et Google Chrome, Chromium ou Microsoft Edge installés.")

    out = src.with_suffix(".pdf")
    today = datetime.date.today().strftime("%d/%m/%Y")
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        app = KIT / "app"
        head = tmp / "head.html"   # neutralise la mise en page par défaut de pandoc
        head.write_text("<style>html,body{max-width:none;margin:0;padding:0;background:#fff}</style>", encoding="utf-8")
        before = tmp / "before.html"
        before.write_text(f'<div class="doc"><div class="doc-brand"><img src="img/logo-decideom.png" alt="DecideOm">'
                          f'<div class="who"><b>{html.escape(args.client)}</b>{today}</div></div>', encoding="utf-8")
        after = tmp / "after.html"
        after.write_text("</div>", encoding="utf-8")
        page = tmp / "cr.html"
        subprocess.run(["pandoc", str(src), "-f", "gfm+implicit_figures", "-t", "html5", "-s",
                        "--embed-resources", "--css", str(app / "theme.css"), "--css", str(app / "document.css"),
                        "--resource-path", os.pathsep.join([str(src.parent), str(app)]),
                        "--metadata", f"pagetitle={src.stem}", "--metadata", "lang=fr",
                        "--include-in-header", str(head), "--include-before-body", str(before),
                        "--include-after-body", str(after), "-o", str(page)],
                       check=True)
        subprocess.run([chrome, "--headless=new", "--disable-gpu", "--no-pdf-header-footer",
                        f"--user-data-dir={tmp / 'profile'}", "--no-first-run",
                        f"--print-to-pdf={out}", page.as_uri()],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print(f"PDF : {out}")


if __name__ == "__main__":
    main()
