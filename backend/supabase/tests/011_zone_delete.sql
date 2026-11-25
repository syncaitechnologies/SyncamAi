-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(3);

select ok(has_table_privilege('syncam_render', 'config.zones', 'DELETE'),
  'the API runtime role can remove zones');
select ok(not has_table_privilege('authenticated', 'config.zones', 'DELETE')
  and not has_table_privilege('anon', 'config.zones', 'DELETE'),
  'browser roles cannot remove zones');
select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'config.zones'::regclass),
  'removals stay inside the workspace''s row-level security');

select * from finish();
rollback;
