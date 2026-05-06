package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGKnowledgeGraphStore) UpsertRelation(ctx context.Context, relation *store.Relation) error {
	aid, err := parseUUID(relation.AgentID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: agent: %w", err)
	}
	src, err := parseUUID(relation.SourceEntityID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: source: %w", err)
	}
	tgt, err := parseUUID(relation.TargetEntityID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: target: %w", err)
	}
	props, err := json.Marshal(relation.Properties)
	if err != nil {
		props = []byte("{}")
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_relations
			(id, agent_id, user_id, source_entity_id, relation_type, target_entity_id, confidence, properties, tenant_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
			confidence  = EXCLUDED.confidence,
			properties  = EXCLUDED.properties,
			tenant_id   = EXCLUDED.tenant_id`,
		id, aid, relation.UserID, src, relation.RelationType, tgt, relation.Confidence, props, tid, now,
	)
	return err
}

func (s *PGKnowledgeGraphStore) DeleteRelation(ctx context.Context, agentID, userID, relationID string) error {
	aid, err := parseUUID(agentID)
	if err != nil {
		return fmt.Errorf("kg delete relation: agent: %w", err)
	}
	rid, err := parseUUID(relationID)
	if err != nil {
		return fmt.Errorf("kg delete relation: id: %w", err)
	}
	userWhere, userArgs := kgUserWhere(ctx, userID, 3)
	args := append([]any{rid, aid}, userArgs...)
	tc, tcArgs, _, err := scopeClause(ctx, 3+len(userArgs))
	if err != nil {
		return err
	}
	args = append(args, tcArgs...)
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM kg_relations WHERE id = $1 AND agent_id = $2`+userWhere+tc,
		args...,
	)
	return err
}

func (s *PGKnowledgeGraphStore) ListRelations(ctx context.Context, agentID, userID, entityID string) ([]store.Relation, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list relations: agent: %w", err)
	}
	eid, err := parseUUID(entityID)
	if err != nil {
		return nil, fmt.Errorf("kg list relations: entity: %w", err)
	}

	userWhere, userArgs := kgUserWhere(ctx, userID, 2)
	eidIdx := 2 + len(userArgs)
	args := append([]any{aid}, userArgs...)
	args = append(args, eid)
	tc, tcArgs, _, err := scopeClause(ctx, eidIdx+1)
	if err != nil {
		return nil, err
	}
	args = append(args, tcArgs...)

	q := fmt.Sprintf(`SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations
		WHERE agent_id = $1%s AND valid_until IS NULL
		  AND (source_entity_id = $%d OR target_entity_id = $%d)`+tc+`
		ORDER BY created_at DESC`, userWhere, eidIdx, eidIdx)

	var rRows []relationRow
	if err := pkgSqlxDB.SelectContext(ctx, &rRows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.Relation, len(rRows))
	for i := range rRows {
		result[i] = rRows[i].toRelation()
	}
	return result, nil
}

func (s *PGKnowledgeGraphStore) ListAllRelations(ctx context.Context, agentID, userID string, limit int) ([]store.Relation, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list all relations: %w", err)
	}
	if limit <= 0 {
		limit = 200
	}
	where := "agent_id = $1 AND valid_until IS NULL"
	args := []any{aid}
	idx := 2
	userWhere, userArgs := kgUserWhere(ctx, userID, idx)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		idx += len(userArgs)
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx++
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations WHERE %s
		ORDER BY created_at DESC LIMIT $%d`, where, idx)
	var rRows []relationRow
	if err = pkgSqlxDB.SelectContext(ctx, &rRows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.Relation, len(rRows))
	for i := range rRows {
		result[i] = rRows[i].toRelation()
	}
	return result, nil
}

func (s *PGKnowledgeGraphStore) IngestExtraction(ctx context.Context, agentID, userID string, entities []store.Entity, relations []store.Relation) ([]string, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg ingest extraction: agent: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now()
	tid := tenantIDForInsert(ctx)

	// Batch upsert entities using unnest for fewer DB round trips.
	extIDToUUID := make(map[string]uuid.UUID, len(entities))
	if len(entities) > 0 {
		ids := make([]string, len(entities))
		extIDs := make([]string, len(entities))
		names := make([]string, len(entities))
		entTypes := make([]string, len(entities))
		descs := make([]string, len(entities))
		propsJSON := make([]string, len(entities))
		srcIDs := make([]string, len(entities))
		confidences := make([]float64, len(entities))
		eventTimes := make([]*time.Time, len(entities))

		for i, e := range entities {
			entities[i].AgentID = agentID
			entities[i].UserID = userID
			props, _ := json.Marshal(e.Properties)
			ids[i] = uuid.Must(uuid.NewV7()).String()
			extIDs[i] = e.ExternalID
			names[i] = e.Name
			entTypes[i] = e.EntityType
			descs[i] = e.Description
			propsJSON[i] = string(props)
			srcIDs[i] = e.SourceID
			confidences[i] = e.Confidence
			eventTimes[i] = e.EventTime
		}

		rows, err := tx.QueryContext(ctx, `
			WITH new_e AS (
				SELECT * FROM unnest(
					$1::uuid[], $2::uuid[], $3::text[], $4::text[], $5::text[],
					$6::text[], $7::text[], $8::text[], $9::text[], $10::float8[],
					$11::uuid[], $12::timestamptz[], $13::timestamptz[]
				) AS t(id, agent_id, user_id, external_id, name, entity_type,
				       description, properties, source_id, confidence, tenant_id,
				       created_at, event_time)
			)
			INSERT INTO kg_entities
				(id, agent_id, user_id, external_id, name, entity_type, description,
				 properties, source_id, confidence, tenant_id, created_at, updated_at, event_time)
			SELECT id, agent_id, user_id, external_id, name, entity_type, description,
			       properties::jsonb, source_id, confidence, tenant_id, created_at, created_at, event_time
			FROM new_e
			ON CONFLICT (agent_id, user_id, external_id) DO UPDATE SET
				name        = EXCLUDED.name,
				entity_type = EXCLUDED.entity_type,
				description = EXCLUDED.description,
				properties  = EXCLUDED.properties,
				source_id   = EXCLUDED.source_id,
				confidence  = EXCLUDED.confidence,
				tenant_id   = EXCLUDED.tenant_id,
				updated_at  = EXCLUDED.updated_at,
				event_time  = CASE WHEN EXCLUDED.event_time IS NOT NULL THEN EXCLUDED.event_time ELSE kg_entities.event_time END
			RETURNING id, external_id`,
			pq.Array(ids), pq.Array(makeUUIDArr(len(entities), aid)), pq.Array(makeStringArr(len(entities), userID)),
			pq.Array(extIDs), pq.Array(names), pq.Array(entTypes),
			pq.Array(descs), pq.Array(propsJSON), pq.Array(srcIDs), pq.Array(confidences),
			pq.Array(makeUUIDArr(len(entities), tid)), pq.Array(makeTimeArr(len(entities), now)),
			pq.Array(eventTimes),
		)
		if err != nil {
			return nil, fmt.Errorf("kg batch entity upsert: %w", err)
		}
		for rows.Next() {
			var actualID uuid.UUID
			var extID string
			if err := rows.Scan(&actualID, &extID); err != nil {
				rows.Close()
				return nil, err
			}
			extIDToUUID[extID] = actualID
		}
		rows.Close()
	}

	// Batch upsert relations using unnest.
	if len(relations) > 0 {
		relIDs := make([]string, 0, len(relations))
		relSrcIDs := make([]string, 0, len(relations))
		relTypes := make([]string, 0, len(relations))
		relTgtIDs := make([]string, 0, len(relations))
		relConfs := make([]float64, 0, len(relations))
		relProps := make([]string, 0, len(relations))

		for _, r := range relations {
			src, ok1 := extIDToUUID[r.SourceEntityID]
			tgt, ok2 := extIDToUUID[r.TargetEntityID]
			if !ok1 || !ok2 {
				continue
			}
			props, _ := json.Marshal(r.Properties)
			relIDs = append(relIDs, uuid.Must(uuid.NewV7()).String())
			relSrcIDs = append(relSrcIDs, src.String())
			relTypes = append(relTypes, r.RelationType)
			relTgtIDs = append(relTgtIDs, tgt.String())
			relConfs = append(relConfs, r.Confidence)
			relProps = append(relProps, string(props))
		}

		if len(relIDs) > 0 {
			_, err := tx.ExecContext(ctx, `
				WITH new_r AS (
					SELECT * FROM unnest(
						$1::uuid[], $2::uuid[], $3::text[], $4::uuid[], $5::text[],
						$6::uuid[], $7::float8[], $8::text::jsonb[], $9::uuid[], $10::timestamptz[]
					) AS t(id, agent_id, user_id, source_entity_id, relation_type,
					       target_entity_id, confidence, properties, tenant_id, created_at)
				)
				INSERT INTO kg_relations
					(id, agent_id, user_id, source_entity_id, relation_type,
					 target_entity_id, confidence, properties, tenant_id, created_at)
				SELECT id, agent_id, user_id, source_entity_id, relation_type,
				       target_entity_id, confidence, properties, tenant_id, created_at
				FROM new_r
				ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
					confidence  = EXCLUDED.confidence,
					properties  = EXCLUDED.properties,
					tenant_id   = EXCLUDED.tenant_id`,
				pq.Array(relIDs), pq.Array(makeUUIDStrArr(len(relIDs), aid.String())),
				pq.Array(makeStringArr(len(relIDs), userID)), pq.Array(relSrcIDs),
				pq.Array(relTypes), pq.Array(relTgtIDs), pq.Array(relConfs),
				pq.Array(relProps), pq.Array(makeUUIDStrArr(len(relIDs), tid.String())),
				pq.Array(makeTimeArr(len(relIDs), now)),
			)
			if err != nil {
				return nil, fmt.Errorf("kg batch relation upsert: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Collect upserted entity IDs for downstream processing (e.g. dedup)
	entityIDs := make([]string, 0, len(extIDToUUID))
	for _, uid := range extIDToUUID {
		entityIDs = append(entityIDs, uid.String())
	}

	// Generate embeddings post-commit (best-effort, outside transaction).
	if s.embProvider != nil && len(extIDToUUID) > 0 {
		s.generateEmbeddingsAsync(ctx, entities, extIDToUUID)
	}

	return entityIDs, nil
}

// generateEmbeddingsAsync generates embeddings for entities outside the main transaction.
// Failures are logged and skipped — BackfillKGEmbeddings handles missing ones.
func (s *PGKnowledgeGraphStore) generateEmbeddingsAsync(ctx context.Context, entities []store.Entity, extIDToUUID map[string]uuid.UUID) {
	texts := make([]string, 0, len(entities))
	ids := make([]uuid.UUID, 0, len(entities))
	for _, e := range entities {
		if uid, ok := extIDToUUID[e.ExternalID]; ok {
			texts = append(texts, e.Name+" "+e.Description)
			ids = append(ids, uid)
		}
	}
	if len(texts) == 0 {
		return
	}

	embeddings, embErr := s.embProvider.Embed(ctx, texts)
	if embErr != nil {
		slog.Warn("kg entity embedding batch failed", "error", embErr)
		return
	}

	// Batch update embeddings using unnest.
	vecStrs := make([]string, 0, len(embeddings))
	vecIDs := make([]string, 0, len(embeddings))
	for i, emb := range embeddings {
		if len(emb) == 0 {
			continue
		}
		vecStrs = append(vecStrs, vectorToString(emb))
		vecIDs = append(vecIDs, ids[i].String())
	}
	if len(vecStrs) == 0 {
		return
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE kg_entities e SET embedding = d.vec::vector
		FROM (SELECT * FROM unnest($1::uuid[], $2::text[]) AS d(id, vec))
		WHERE e.id = d.id::uuid`,
		pq.Array(vecIDs), pq.Array(vecStrs),
	); err != nil {
		slog.Warn("kg entity embedding batch update failed", "error", err)
	}
}

// makeUUIDArr returns a slice with the same UUID repeated n times.
func makeUUIDArr(n int, v uuid.UUID) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = v.String()
	}
	return s
}

// makeUUIDStrArr returns a slice with the same UUID string repeated n times.
func makeUUIDStrArr(n int, v string) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// makeStringArr returns a slice with the same string repeated n times.
func makeStringArr(n int, v string) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// makeTimeArr returns a slice with the same time repeated n times.
func makeTimeArr(n int, v time.Time) []time.Time {
	s := make([]time.Time, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func (s *PGKnowledgeGraphStore) PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, fmt.Errorf("kg prune: %w", err)
	}
	userWhere, userArgs := kgUserWhere(ctx, userID, 2)
	args := append([]any{aid}, userArgs...)
	confIdx := 2 + len(userArgs)
	args = append(args, minConfidence)
	tc, tcArgs, _, tcErr := scopeClause(ctx, confIdx+1)
	if tcErr != nil {
		return 0, tcErr
	}
	args = append(args, tcArgs...)

	var res sql.Result
	res, err = s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM kg_entities WHERE agent_id = $1%s AND confidence < $%d`, userWhere, confIdx)+tc,
		args...,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *PGKnowledgeGraphStore) ClearAll(ctx context.Context, agentID, userID string) (int, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, fmt.Errorf("kg clear all: %w", err)
	}
	userWhere, userArgs := kgUserWhere(ctx, userID, 2)
	tc, tcArgs, _, tcErr := scopeClause(ctx, 2+len(userArgs))
	if tcErr != nil {
		return 0, tcErr
	}
	baseArgs := append([]any{aid}, userArgs...)
	args := append(baseArgs, tcArgs...)

	where := fmt.Sprintf("WHERE agent_id = $1%s", userWhere) + tc

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var total int
	for _, table := range []string{"kg_dedup_candidates", "kg_relations", "kg_entities"} {
		res, delErr := tx.ExecContext(ctx, "DELETE FROM "+table+" "+where, args...)
		if delErr != nil {
			return 0, delErr
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, tx.Commit()
}

func (s *PGKnowledgeGraphStore) Stats(ctx context.Context, agentID, userID string) (*store.GraphStats, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg stats: %w", err)
	}
	stats := &store.GraphStats{EntityTypes: make(map[string]int)}

	userFilter := ""
	args := []any{aid}
	idx := 2
	if userID != "" {
		userFilter = fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	tenantFilter := tc
	args = append(args, tcArgs...)

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_entities WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter, args...,
	).Scan(&stats.EntityCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_relations WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter, args...,
	).Scan(&stats.RelationCount); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, COUNT(*) FROM kg_entities WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter+` GROUP BY entity_type`, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			continue
		}
		stats.EntityTypes[t] = c
	}

	// Fetch distinct user IDs (only when not filtering by specific user)
	if userID == "" {
		uidRows, uidErr := s.db.QueryContext(ctx,
			`SELECT DISTINCT user_id FROM kg_entities WHERE agent_id = $1`+tenantFilter+` AND user_id != '' ORDER BY user_id`,
			append([]any{aid}, tcArgs...)...,
		)
		if uidErr == nil {
			defer uidRows.Close()
			for uidRows.Next() {
				var uid string
				if uidRows.Scan(&uid) == nil && uid != "" {
					stats.UserIDs = append(stats.UserIDs, uid)
				}
			}
		}
	}

	return stats, nil
}

func (s *PGKnowledgeGraphStore) Close() error { return nil }
