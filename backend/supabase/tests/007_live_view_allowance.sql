-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(10);

select is((select column_default from information_schema.columns
  where table_schema = 'identity' and table_name = 'tenants' and column_name = 'live_view_minutes'),
  '10', 'new workspaces get 10 live-view minutes a month by default');
select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'platform.live_view_usage'::regclass),
  'live-view usage enforces row level security');
select ok(has_table_privilege('syncam_render', 'platform.live_view_usage', 'SELECT')
  and has_table_privilege('syncam_render', 'platform.live_view_usage', 'INSERT')
  and has_table_privilege('syncam_render', 'platform.live_view_usage', 'UPDATE'),
  'the API runtime role can read and add usage');
select ok(not has_table_privilege('syncam_render', 'platform.live_view_usage', 'DELETE'),
  'the API runtime role cannot delete usage');
select ok(not has_table_privilege('anon', 'platform.live_view_usage', 'SELECT')
  and not has_table_privilege('authenticated', 'platform.live_view_usage', 'SELECT'),
  'browser roles cannot read usage');

insert into identity.tenants (id, name, slug) values
 ('41700000-0000-4000-8000-000000000001', 'Live view A', 'live-view-a'),
 ('41700000-0000-4000-8000-000000000002', 'Live view B', 'live-view-b');
select is((select live_view_minutes from identity.tenants where id = '41700000-0000-4000-8000-000000000001'),
  10, 'a new tenant row starts with the default allowance');
select throws_ok($$update identity.tenants set live_view_minutes = -1 where id = '41700000-0000-4000-8000-000000000001'$$,
  '23514', null, 'a negative allowance is rejected');

create temporary table usage_observations (name text primary key, passed boolean);
grant insert on usage_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
set local app.tenant_id = '41700000-0000-4000-8000-000000000001';
insert into platform.live_view_usage (tenant_id, month, seconds_used)
  values ('41700000-0000-4000-8000-000000000001', '2026-11-01', 30)
  on conflict (tenant_id, month) do update set seconds_used = platform.live_view_usage.seconds_used + excluded.seconds_used;
insert into platform.live_view_usage (tenant_id, month, seconds_used)
  values ('41700000-0000-4000-8000-000000000001', '2026-11-01', 45)
  on conflict (tenant_id, month) do update set seconds_used = platform.live_view_usage.seconds_used + excluded.seconds_used;
insert into usage_observations values ('adds_up', (select seconds_used = 75 from platform.live_view_usage));
do $$ begin
  begin
    insert into platform.live_view_usage (tenant_id, month, seconds_used)
      values ('41700000-0000-4000-8000-000000000002', '2026-11-01', 10);
    insert into usage_observations values ('foreign_write', false);
  exception when insufficient_privilege then
    insert into usage_observations values ('foreign_write', true);
  end;
end $$;
reset role;
select ok((select passed from usage_observations where name = 'adds_up'), 'usage for a month adds up');
select ok((select passed from usage_observations where name = 'foreign_write'), 'the runtime cannot write another workspace''s usage');
select throws_ok($$insert into platform.live_view_usage (tenant_id, month, seconds_used)
  values ('41700000-0000-4000-8000-000000000002', '2026-11-15', 1)$$,
  '23514', null, 'usage is kept per calendar month');

select * from finish();
rollback;
