#!/usr/bin/env python3
"""Offline real Audit constructors, row/HTTP encoders, local JSON Schema."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path('/workspace/agenteam-secret-variable-audit')
OWN = Path('/workspace/agenteam-work-ui')
GO = '/workspace/toolchains/go1.27.1/bin/go'
CONTRACT = r'''package contract
import (
 "testing"
 f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)
func TestIndependentSecretActionMetadataIdentity(t *testing.T) {
 const id = "01900000-0000-7000-8000-000000000006"
 actions := []Action{ProjectVariableCreate, ProjectVariableUpdate, ProjectVariableDelete, ProjectSecretVariableCreate, ProjectSecretVariableUpdate, ProjectSecretVariableDelete}
 metadata := make([]Metadata, len(actions))
 for n, action := range actions {
  version, fields := f.Version(2), []string{"name", "value"}
  if n%3 == 0 { version, fields = 1, []string{"created"} }
  if n%3 == 2 { fields = []string{"deleted"} }
  var err error
  if n < 3 { metadata[n], err = ProjectVariableMetadata(action, ProjectVariableMetadataFields{id,version,fields})
  } else { metadata[n], err = ProjectSecretVariableMetadata(action, ProjectSecretVariableMetadataFields{id,version,fields}) }
  if err != nil { t.Fatal(err) }
 }
 resource, _ := NewResource(ProjectVariableResource, id)
 for n, action := range actions {
  for m := range metadata {
   entry := projectEntry(t, ProjectUpdate)
   entry.Action, entry.Resource, entry.Metadata = action, resource, metadata[m]
   _, err := NewEntry(entry)
   if (err == nil) != (n == m) { t.Fatalf("cross-family/action metadata substitution: %d/%d", n,m) }
  }
 }
 t.Log("36 actual Entry/Metadata identity combinations: only six exact original action pairs accepted")
}
'''
HTTP = r'''package audithttp
import (
 "testing"
 c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
 identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)
func TestIndependentSecretFilterDoesNotWidenSystemRecord(t *testing.T) {
 for _, action := range []string{"create", "update", "delete"} {
  version, fields := "2", `["value"]`
  if action == "create" { version, fields = "1", `["created"]` }
  if action == "delete" { fields = `["deleted"]` }
  full := "project.secret_variable."+action
  tc := typedCase{full,"project_variable",`{"variable_id":"`+wireID+`","version":"`+version+`","changed_fields":`+fields+`}`,"",c.Success}
  record := projectAuditTestRecord(t,tc,identity.Human,false)
  record.Scope = identity.SystemScope()
  if _, err := projectRecord(record); err == nil { t.Fatal("Secret Variable accepted in System record") }
  if _, _, err := query("action="+full+"&resource_kind=project_variable",false); err != nil { t.Fatal("approved shared filter rejected") }
 }
}
'''
env = os.environ.copy()
env.update({'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off',
 'GOTELEMETRY':'off','GOMAXPROCS':'2','GOFLAGS':'-mod=readonly',
 'GOCACHE':str(OWN/'output/ai/work-owner-planning-ui/implementation/gocache'),
 'GOMODCACHE':'/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
 'AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON':'/usr/bin/python3'})
output = OWN/'output/ai/work-owner-planning-ui/secret-audit-review'
output.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(dir=output) as tmp:
 tmp=Path(tmp)
 replace={}
 for package, text in [('contract',CONTRACT),('http',HTTP)]:
  path=tmp/(package+'_review_test.go'); path.write_text(text)
  replace[str(ROOT/'internal/central/audit'/package/'independent_review_test.go')]=str(path)
 overlay=tmp/'overlay.json'; overlay.write_text(json.dumps({'Replace':replace}))
 command=[GO,'test','-race','-count=1','-p=1','-timeout=45s','-v','-overlay='+str(overlay),
 '-run=^(TestIndependentSecret.*|TestProjectSecretVariable(AuditClosedReadContract|ActualRowDecoder|AuditWireAndSchema))$',
 './internal/central/audit/contract','./internal/central/audit','./internal/central/audit/http']
 result=subprocess.run(command,cwd=ROOT,env=env,timeout=90)
 raise SystemExit(result.returncode)
