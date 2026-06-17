package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RawMessageChunk represents a chunked embedding derived from raw messages.
type RawMessageChunk struct {
	ID           string    `json:"id" db:"id"`
	AgentID      string    `json:"agent_id" db:"agent_id"`
	GraphID      string    `json:"graph_id" db:"graph_id"`
	ChatID       string    `json:"chat_id" db:"chat_id"`
	ChatName     string    `json:"chat_name" db:"chat_name"`
	Sender       string    `json:"sender" db:"sender"`
	SenderID     string    `json:"sender_id" db:"sender_id"`
	MsgTimeFrom  time.Time `json:"msg_time_from" db:"msg_time_from"`
	MsgTimeTo    time.Time `json:"msg_time_to" db:"msg_time_to"`
	ChunkIndex   int       `json:"chunk_index" db:"chunk_index"`
	Text         string    `json:"text" db:"text"`
	ContentHash  string    `json:"content_hash" db:"content_hash"`
	SourceMsgIDs []string  `json:"source_msg_ids" db:"source_msg_ids"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	HasEmbedding bool      `json:"has_embedding" db:"-"`
}

// RawMessageChunkSearchResult is a single result from hybrid search.
type RawMessageChunkSearchResult struct {
	Chunk      RawMessageChunk `json:"chunk"`
	Score      float64         `json:"score"`
	FTSRank    int             `json:"fts_rank,omitempty"`
	VectorRank int             `json:"vector_rank,omitempty"`
}

// RawMessageChunkSearchOptions configures a search query.
type RawMessageChunkSearchOptions struct {
	MaxResults int        // default 10
	MinScore   float64    // default 0 (no filter)
	ChatID     string     // filter to specific chat
	GraphID    string     // filter to specific graph scope
	FromTime   *time.Time // inclusive lower bound on msg_time_from
	ToTime     *time.Time // inclusive upper bound on msg_time_to
	RRFK       int        // RRF constant (default 60)
}

// RawMessageChunkListOpts configures a paginated list query.
type RawMessageChunkListOpts struct {
	Limit        int
	Offset       int
	AgentID      string
	ChatID       string
	GraphID      string
	Sender       string // substring (ILIKE) match over sender — was exact; see SRS 008 FR-03
	HasEmbedding *bool  // nil=all, true=has embedding, false=no embedding
	FromTime     *time.Time
	ToTime       *time.Time

	// SearchText is a substring (ILIKE) match over the chunk text. Empty = not
	// applied. Server-side so it participates in the COUNT/paging (it used to
	// filter the fetched page only). See SRS 008 FR-00/FR-01.
	SearchText string
}

// RawMessageChunkStore manages chunked embeddings from raw messages.
type RawMessageChunkStore interface {
	// StoreChunks persists chunks with their embeddings.
	StoreChunks(ctx context.Context, chunks []RawMessageChunk, embeddings [][]float32) error

	// Search performs hybrid FTS + vector search with Reciprocal Rank Fusion.
	Search(ctx context.Context, query, agentID string, opts RawMessageChunkSearchOptions) ([]RawMessageChunkSearchResult, error)

	// List returns paginated chunks with optional filters.
	List(ctx context.Context, opts RawMessageChunkListOpts) ([]RawMessageChunk, int, error)

	// DeleteByGraphID removes all chunks for a given agent+graph scope.
	DeleteByGraphID(ctx context.Context, agentID, graphID string) error

	// DeleteByIDs removes chunks by their IDs.
	DeleteByIDs(ctx context.Context, ids []string) (int64, error)

	// DeleteByChatID removes all chunks for a given agent+chat scope.
	DeleteByChatID(ctx context.Context, agentID, chatID string) (int64, error)

	// DeleteBySourceMsgIDs removes every chunk whose source_msg_ids array overlaps
	// any of the given listen-raw-message IDs, and returns the UNION of all source
	// message IDs that those deleted chunks covered — including day-group neighbor
	// messages whose chunks were co-deleted with the edited message's chunks. The
	// caller resets embedded_at for the neighbor IDs (those in the result but not in
	// the input set) so the embedding worker re-embeds them. Used by the scope-edit
	// "true move" (SRS 007 FR-08). Tenant-scoped.
	DeleteBySourceMsgIDs(ctx context.Context, msgIDs []uuid.UUID) (sources []uuid.UUID, deleted int64, err error)

	// ReEmbedChunks generates embeddings for chunks that lack them, scoped by opts filters.
	ReEmbedChunks(ctx context.Context, opts RawMessageChunkListOpts) (processed int, failed int, err error)

	// SetEmbeddingProvider configures the embedding provider.
	SetEmbeddingProvider(provider EmbeddingProvider)
}
