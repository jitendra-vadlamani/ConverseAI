-- Seed Topics
INSERT INTO topics (id, name, level, description, artifact_type) VALUES
('t_dsa', 'Data Structures & Algorithms', 1, 'Core algorithmic problem solving.', NULL),
('t_dsa_arrays', 'Arrays & Strings', 2, 'Fundamental contiguous memory structures.', 'practice_problem'),
('t_dsa_graphs', 'Graphs', 2, 'Nodes and edges representation.', 'practice_problem'),
('t_sd', 'System Design', 1, 'Architecting scalable software systems.', NULL),
('t_sd_db', 'Databases', 2, 'Relational vs NoSQL, scaling, ACID.', 'concept_review'),
('t_sd_dist', 'Distributed Systems', 2, 'Consensus, replication, partitioning.', 'concept_review');

-- Seed Edges
INSERT INTO topic_edges (from_id, to_id, edge_type) VALUES
('t_dsa_arrays', 't_dsa', 'part_of'),
('t_dsa_graphs', 't_dsa', 'part_of'),
('t_dsa_arrays', 't_dsa_graphs', 'prerequisite_of'),
('t_sd_db', 't_sd', 'part_of'),
('t_sd_dist', 't_sd', 'part_of'),
('t_sd_db', 't_sd_dist', 'prerequisite_of');

-- Seed Progress
INSERT INTO progress (topic_id, mastery_score, last_reviewed, notes) VALUES
('t_dsa_arrays', 80, CURRENT_TIMESTAMP, 'Comfortable with two-pointers and sliding window.'),
('t_dsa_graphs', 30, CURRENT_TIMESTAMP, 'Struggle with Dijkstra and A*.'),
('t_sd_db', 60, CURRENT_TIMESTAMP, 'Need to review B-Trees vs LSM Trees.'),
('t_sd_dist', 10, CURRENT_TIMESTAMP, 'Just started Paxos/Raft.');
