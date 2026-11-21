-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(9);

select ok((select prosecdef from pg_proc where oid = 'platform.site_box_transitions(interval)'::regprocedure),
  'presence changes are read by a narrow definer function');
select ok(has_function_privilege('syncam_render', 'platform.site_box_transitions(interval)', 'EXECUTE'),
  'the API runtime role can run the watchdog check');
select ok(not has_function_privilege('anon', 'platform.site_box_transitions(interval)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'platform.site_box_transitions(interval)', 'EXECUTE'),
  'browser roles cannot run it');

insert into identity.tenants (id, name, slug) values
 ('41800000-0000-4000-8000-000000000001', 'Presence test', 'presence-test');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41800000-0000-4000-8000-000000000011', '41800000-0000-4000-8000-000000000001', 'Warehouse', 'America/Toronto', 'test');
insert into platform.site_keys (id, tenant_id, site_id, label, key_hash, created_by, last_seen_at) values
 ('41800000-0000-4000-8000-000000000021', '41800000-0000-4000-8000-000000000001', '41800000-0000-4000-8000-000000000011',
  'Front PC', repeat('a', 64), 'test', clock_timestamp() - interval '15 minutes'),
 ('41800000-0000-4000-8000-000000000022', '41800000-0000-4000-8000-000000000001', '41800000-0000-4000-8000-000000000011',
  'Back PC', repeat('b', 64), 'test', clock_timestamp() - interval '1 minute'),
 ('41800000-0000-4000-8000-000000000023', '41800000-0000-4000-8000-000000000001', '41800000-0000-4000-8000-000000000011',
  'Never paired', repeat('c', 64), 'test', null);

create temporary table presence_results (round int, kind text, key_id uuid, workspace text, site_name text, timezone text, box_label text);
grant insert on presence_results to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
insert into presence_results select 1, kind, key_id, workspace, site_name, timezone, box_label from platform.site_box_transitions(interval '10 minutes');
insert into presence_results select 2, kind, key_id, workspace, site_name, timezone, box_label from platform.site_box_transitions(interval '10 minutes');
reset role;

select is((select count(*)::int from presence_results where round = 1 and kind = 'offline'
  and key_id = '41800000-0000-4000-8000-000000000021' and workspace = 'Presence test'
  and site_name = 'Warehouse' and timezone = 'America/Toronto' and box_label = 'Front PC'),
  1, 'a box silent for 15 minutes is reported offline with its names');
select is((select count(*)::int from presence_results where round = 1), 1,
  'a fresh box and a never-paired key are not reported');
select is((select count(*)::int from presence_results where round = 2), 0,
  'an offline box is reported only once');

update platform.site_keys set last_seen_at = clock_timestamp() where id = '41800000-0000-4000-8000-000000000021';
set local role syncam_render;
insert into presence_results select 3, kind, key_id, workspace, site_name, timezone, box_label from platform.site_box_transitions(interval '10 minutes');
reset role;
select is((select count(*)::int from presence_results where round = 3 and kind = 'online'
  and key_id = '41800000-0000-4000-8000-000000000021'), 1, 'the box coming back is reported once');
select is((select offline_since from platform.site_keys where id = '41800000-0000-4000-8000-000000000021'),
  null, 'the offline mark is cleared');
select throws_ok($$select * from platform.site_box_transitions(interval '1 minute')$$,
  '22023', null, 'a threshold shorter than two heartbeats is refused');

select * from finish();
rollback;
