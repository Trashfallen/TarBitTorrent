"""Пишет build/version_info.txt (свойства exe: версия, название) из tarbit/version.py."""

from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ns: dict = {}
exec((ROOT / "tarbit" / "version.py").read_text(encoding="utf-8"), ns)
version = ns["VERSION"]
parts = [int(x) for x in version.split(".")] + [0] * 4
v4 = tuple(parts[:4])

text = f"""VSVersionInfo(
  ffi=FixedFileInfo(filevers={v4}, prodvers={v4}, mask=0x3f, flags=0x0, OS=0x40004,
                    fileType=0x1, subtype=0x0, date=(0, 0)),
  kids=[
    StringFileInfo([StringTable('041904B0', [
      StringStruct('CompanyName', 'Trashfallen'),
      StringStruct('FileDescription', 'TarBitTorrent'),
      StringStruct('FileVersion', '{version}'),
      StringStruct('InternalName', 'TarBitTorrent'),
      StringStruct('OriginalFilename', 'TarBitTorrent.exe'),
      StringStruct('ProductName', 'TarBitTorrent'),
      StringStruct('ProductVersion', '{version}')])]),
    VarFileInfo([VarStruct('Translation', [1049, 1200])])
  ]
)
"""
out = ROOT / "build" / "version_info.txt"
out.parent.mkdir(exist_ok=True)
out.write_text(text, encoding="utf-8")
print(version)
