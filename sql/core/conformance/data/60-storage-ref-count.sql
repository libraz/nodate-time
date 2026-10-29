-- storage_objects.ref_count is the number of rows, in any layer and in any
-- state (reserved, live, retired), whose foreign key points at the object
-- (PROTOCOL.md §Storage). The unreferenced sweep deletes on ref_count = 0
-- with no age condition, so a count below the truth deletes a blob still
-- in use and a count above it leaks one forever.
--
-- The referencing columns are read from information_schema rather than
-- listed, so a product layer adding a reference is counted without
-- editing this file. Every rule is included: RESTRICT, CASCADE and
-- SET NULL references all count while they are set.

SET SESSION group_concat_max_len = 1048576;

SET @nf_so_refs := (
  SELECT GROUP_CONCAT(
           CONCAT('SELECT `', k.column_name, '` AS so_id FROM `', k.table_name,
                  '` WHERE `', k.column_name, '` IS NOT NULL')
           ORDER BY k.table_name, k.column_name SEPARATOR ' UNION ALL ')
  FROM information_schema.key_column_usage k
  WHERE k.table_schema = DATABASE()
    AND k.referenced_table_schema = DATABASE()
    AND k.referenced_table_name = 'storage_objects'
    AND k.referenced_column_name = 'id');

-- Core itself declares references, so finding none means the lookup is
-- broken, and the comparison below would pass over nothing.
CALL nf_conformance_assert(
  @nf_so_refs IS NOT NULL,
  '60 found no foreign key referencing storage_objects(id)');

SET @nf_so_mismatch := NULL;
SET @nf_so_sql := CONCAT(
  'SELECT GROUP_CONCAT(CONCAT(so.id, '' (ref_count '', so.ref_count, '', rows '', IFNULL(r.n, 0), '')'') ',
  'ORDER BY so.id SEPARATOR '', '') INTO @nf_so_mismatch ',
  'FROM storage_objects so LEFT JOIN (SELECT so_id, COUNT(*) AS n FROM (', @nf_so_refs,
  ') refs GROUP BY so_id) r ON r.so_id = so.id WHERE so.ref_count <> IFNULL(r.n, 0)');
PREPARE nf_so_ps FROM @nf_so_sql;
EXECUTE nf_so_ps;
DEALLOCATE PREPARE nf_so_ps;

CALL nf_conformance_assert(
  @nf_so_mismatch IS NULL,
  CONCAT('60 storage_objects ref_count differs from its referencing rows, by id: ',
         IFNULL(@nf_so_mismatch, '')));

SET @nf_so_refs = NULL;
SET @nf_so_sql = NULL;
SET @nf_so_mismatch = NULL;
