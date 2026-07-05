CREATE TABLE users (
    id VARCHAR(255) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE topics (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    level INTEGER NOT NULL,
    description TEXT,
    artifact_type VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE topic_edges (
    from_id VARCHAR(255) REFERENCES topics(id) ON DELETE CASCADE,
    to_id VARCHAR(255) REFERENCES topics(id) ON DELETE CASCADE,
    edge_type VARCHAR(50) NOT NULL, -- 'prerequisite_of', 'part_of', 'related_to'
    PRIMARY KEY (from_id, to_id, edge_type)
);

CREATE TABLE progress (
    topic_id VARCHAR(255) PRIMARY KEY REFERENCES topics(id) ON DELETE CASCADE,
    mastery_score INTEGER DEFAULT 0 CHECK (mastery_score >= 0 AND mastery_score <= 100),
    last_reviewed TIMESTAMP WITH TIME ZONE,
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
