-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(13);

select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'platform.operators'::regclass),
  'the operator list enforces row level security');
select ok(not has_table_privilege('authenticated', 'platform.operators', 'SELECT')
  and not has_table_privilege('syncam_render', 'platform.operators', 'INSERT'),
  'neither browsers nor the API can read or add operators directly');
select ok(has_function_privilege('syncam_render', 'platform.admin_update_workspace(uuid,uuid,uuid,jsonb)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'platform.admin_update_workspace(uuid,uuid,uuid,jsonb)', 'EXECUTE'),
  'only the API can call the admin routines');

insert into auth.users (id, email) values
 ('41900000-0000-4000-8000-000000000001', 'owner@example.test'),
 ('41900000-0000-4000-8000-000000000002', 'client@example.test');
insert into platform.operators (user_id, note) values ('41900000-0000-4000-8000-000000000001', 'test owner');
insert into identity.tenants (id, name, slug, plan, trial_ends_at, site_limit, camera_limit, live_view_minutes) values
 ('41900000-0000-4000-8000-000000000011', 'Client A', 'client-a', 'trial', clock_timestamp() + interval '3 days', 1, 10, 10);

select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41900000-0000-4000-8000-000000000001', 'claims', '{"app_metadata": {}}'::jsonb)))
  ->'claims'->'app_metadata'->>'syncam_operator', 'true', 'operators get the operator flag');
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41900000-0000-4000-8000-000000000002',
  'claims', '{"app_metadata": {"syncam_operator": true}}'::jsonb)))
  ->'claims'->'app_metadata'->'syncam_operator', null, 'everyone else gets no flag, even a forged one');

create temporary table admin_observations (name text primary key, passed boolean);
grant insert on admin_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
insert into admin_observations values ('owner_lists',
  (select count(*) = 1 from platform.admin_list_workspaces('41900000-0000-4000-8000-000000000001') where slug = 'client-a' and live_view_minutes = 10));
do $$ begin
  begin
    perform * from platform.admin_list_workspaces('41900000-0000-4000-8000-000000000002');
    insert into admin_observations values ('client_refused', false);
  exception when insufficient_privilege then
    insert into admin_observations values ('client_refused', true);
  end;
end $$;
select platform.admin_update_workspace('41900000-0000-4000-8000-000000000001', '41900000-0000-4000-8000-000000000021',
  '41900000-0000-4000-8000-000000000011',
  '{"plan": "active", "trial_ends_at": null, "camera_limit": null, "live_view_minutes": 300}'::jsonb);
do $$ begin
  begin
    perform platform.admin_update_workspace('41900000-0000-4000-8000-000000000001', '41900000-0000-4000-8000-000000000022',
      '41900000-0000-4000-8000-000000000011', '{"tier": "enterprise"}'::jsonb);
    insert into admin_observations values ('unknown_key', false);
  exception when sqlstate '22023' then
    insert into admin_observations values ('unknown_key', true);
  end;
  begin
    perform platform.admin_update_workspace('41900000-0000-4000-8000-000000000001', '41900000-0000-4000-8000-000000000023',
      '41900000-0000-4000-8000-000000000011', '{"live_view_minutes": -5}'::jsonb);
    insert into admin_observations values ('bad_value', false);
  exception when sqlstate '22023' then
    insert into admin_observations values ('bad_value', true);
  end;
  begin
    perform platform.admin_update_workspace('41900000-0000-4000-8000-000000000002', '41900000-0000-4000-8000-000000000024',
      '41900000-0000-4000-8000-000000000011', '{"live_view_minutes": 9999}'::jsonb);
    insert into admin_observations values ('client_update_refused', false);
  exception when insufficient_privilege then
    insert into admin_observations values ('client_update_refused', true);
  end;
end $$;
reset role;

select ok((select passed from admin_observations where name = 'owner_lists'), 'an operator sees every workspace');
select ok((select passed from admin_observations where name = 'client_refused'), 'anyone else is refused the list');
select ok((select passed from admin_observations where name = 'client_update_refused'), 'anyone else is refused changes');
select ok((select passed from admin_observations where name = 'unknown_key'), 'unknown settings are refused');
select ok((select passed from admin_observations where name = 'bad_value'), 'invalid values are refused');
select is((select plan || '/' || coalesce(camera_limit::text, 'unlimited') || '/' || live_view_minutes::text
  from identity.tenants where id = '41900000-0000-4000-8000-000000000011'),
  'active/unlimited/300', 'the change is applied');
select is((select count(*)::int from audit.events where tenant_id = '41900000-0000-4000-8000-000000000011'
  and action = 'tenant.plan.changed' and actor_id = 'operator:41900000-0000-4000-8000-000000000001'
  and before_state->>'plan' = 'trial' and after_state->>'live_view_minutes' = '300'
  and canonical_payload = convert_from(canonical_payload_bytes, 'UTF8')::jsonb
  and record_hash = extensions.digest(previous_hash || extensions.digest(canonical_payload_bytes, 'sha256')
    || convert_to(canonical_payload->>'occurred_at', 'UTF8'), 'sha256')),
  1, 'the change is recorded once in the workspace''s hash-chained audit log');
select is((select count(*)::int from audit.events where tenant_id = '41900000-0000-4000-8000-000000000011'), 1,
  'refused changes leave no audit record');

select * from finish();
rollback;
