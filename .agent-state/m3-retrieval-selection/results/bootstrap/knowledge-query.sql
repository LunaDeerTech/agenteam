SELECT c.id::text,d.content_version,
 c.raw_text,jsonb_build_object('document_id',d.id,'document_version',d.content_version::text,
 'title',d.title,'media_type',d.media_type,'position',c.locator),paradedb.score(c.id) FROM agenteam_knowledge.index_chunks c
 JOIN agenteam_knowledge.index_generations g ON g.project_id=c.project_id AND g.id=c.generation_id
 JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id
 JOIN agenteam_knowledge.index_state st ON st.project_id=g.project_id AND st.serving_profile_id=g.profile_id
 JOIN agenteam_knowledge.index_profiles ip ON ip.project_id=g.project_id AND ip.id=g.profile_id
 WHERE c.project_id=$1 AND g.profile_id=$2 AND g.state='ready' AND ip.state='active'
 AND g.snapshot_id=ip.snapshot_id AND g.dimension=ip.dimension AND c.dimension=ip.dimension
 AND d.status='active' AND d.content_version=g.document_version
 AND ($3::uuid IS NULL OR d.id=$3) AND ($4::text='' OR d.media_type=$4) AND ($5::text='' OR d.source_kind=$5) AND (c.id @@@ paradedb.match('index_text',$6,conjunction_mode=>false)) ORDER BY paradedb.score(c.id) DESC,c.id ASC LIMIT $7;
