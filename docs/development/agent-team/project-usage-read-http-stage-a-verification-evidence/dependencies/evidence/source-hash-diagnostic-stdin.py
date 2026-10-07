from pathlib import Path
import zipfile,hashlib,json
cache=Path('/workspace/go/pkg/mod');prefix='golang.org/x/crypto@v0.55.0/';root=cache/prefix
with zipfile.ZipFile(cache/'cache/download/golang.org/x/crypto/@v/v0.55.0.zip') as z:
 zn=sorted(z.namelist());pn=[prefix+str(p.relative_to(root)) for p in sorted(p for p in root.rglob('*') if p.is_file())]
 differences=[n for n in zn if hashlib.sha256(z.read(n)).digest()!=hashlib.sha256((cache/n).read_bytes()).digest()]
 print(json.dumps({'zip_files':len(zn),'source_files':len(pn),'missing':sorted(set(zn)-set(pn)),'extra':sorted(set(pn)-set(zn)),'content_differences':differences,'first_sort_mismatch':next(([a,b] for a,b in zip(zn,pn) if a!=b),None),'string_sort_matches':sorted(pn)==zn},indent=2))
