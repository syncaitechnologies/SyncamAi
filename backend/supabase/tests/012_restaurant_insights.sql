-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(7);

select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'platform.zone_hours'::regclass),
  'zone counts enforce row level security');
select ok(has_table_privilege('syncam_render', 'platform.zone_hours', 'INSERT')
  and has_table_privilege('syncam_render', 'platform.zone_hours', 'UPDATE')
  and not has_table_privilege('syncam_render', 'platform.zone_hours', 'DELETE'),
  'the API can add and update counts but not delete them');
select ok(not has_table_privilege('authenticated', 'platform.zone_hours', 'SELECT'),
  'browser roles cannot read counts directly');
select ok(pg_get_constraintdef((select oid from pg_constraint where conname = 'zones_kind_check' and conrelid = 'config.zones'::regclass)) like '%door_line%table%',
  'door counter and table zones are allowed');

insert into identity.tenants (id, name, slug) values
 ('41c00000-0000-4000-8000-000000000001', 'Insights A', 'insights-a'),
 ('41c00000-0000-4000-8000-000000000002', 'Insights B', 'insights-b');

create temporary table insight_observations (name text primary key, passed boolean);
grant insert on insight_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
set local app.tenant_id = '41c00000-0000-4000-8000-000000000001';
insert into platform.zone_hours (tenant_id, site_id, zone_id, hour_start, run_id, entries)
  values ('41c00000-0000-4000-8000-000000000001', '41c00000-0000-4000-8000-000000000011', '41c00000-0000-4000-8000-000000000021',
          '2026-11-25 14:00:00+00', '41c00000-0000-4000-8000-000000000031', 12);
insert into insight_observations values ('own_counts', (select count(*) = 1 from platform.zone_hours));
do $$ begin
  begin
    insert into platform.zone_hours (tenant_id, site_id, zone_id, hour_start, run_id)
      values ('41c00000-0000-4000-8000-000000000002', '41c00000-0000-4000-8000-000000000011', '41c00000-0000-4000-8000-000000000022',
              '2026-11-25 14:00:00+00', '41c00000-0000-4000-8000-000000000031');
    insert into insight_observations values ('foreign_counts', false);
  exception when insufficient_privilege then
    insert into insight_observations values ('foreign_counts', true);
  end;
end $$;
reset role;
select ok((select passed from insight_observations where name = 'own_counts'), 'a workspace stores its own counts');
select ok((select passed from insight_observations where name = 'foreign_counts'), 'nobody can store counts for another workspace');
select throws_ok($$insert into platform.zone_hours (tenant_id, site_id, zone_id, hour_start, run_id)
  values ('41c00000-0000-4000-8000-000000000001', '41c00000-0000-4000-8000-000000000011', '41c00000-0000-4000-8000-000000000021',
          '2026-11-25 14:30:00+00', '41c00000-0000-4000-8000-000000000032')$$,
  '23514', null, 'counts are kept per whole hour');

select * from finish();
rollback;
