-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(14);

select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'identity.tenant_features'::regclass)
  and (select relrowsecurity and relforcerowsecurity from pg_class where oid = 'config.feature_settings'::regclass),
  'both feature tables enforce row level security');
select ok(has_table_privilege('syncam_render', 'identity.tenant_features', 'SELECT')
  and not has_table_privilege('syncam_render', 'identity.tenant_features', 'INSERT')
  and not has_table_privilege('syncam_render', 'identity.tenant_features', 'UPDATE'),
  'the API can read plan features but never write them directly');
select ok(has_table_privilege('syncam_render', 'config.feature_settings', 'SELECT')
  and has_table_privilege('syncam_render', 'config.feature_settings', 'INSERT')
  and has_table_privilege('syncam_render', 'config.feature_settings', 'UPDATE')
  and not has_table_privilege('syncam_render', 'config.feature_settings', 'DELETE'),
  'the API can read and change a workspace''s switches');
select ok(not has_table_privilege('authenticated', 'identity.tenant_features', 'SELECT')
  and not has_table_privilege('authenticated', 'config.feature_settings', 'SELECT'),
  'browser roles cannot read feature tables');
select ok(has_function_privilege('syncam_render', 'platform.admin_set_features(uuid,uuid,uuid,jsonb)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'platform.admin_set_features(uuid,uuid,uuid,jsonb)', 'EXECUTE'),
  'only the API can call the owner feature routine');

insert into auth.users (id, email) values
 ('41a00000-0000-4000-8000-000000000001', 'owner@example.test'),
 ('41a00000-0000-4000-8000-000000000002', 'client@example.test');
insert into platform.operators (user_id, note) values ('41a00000-0000-4000-8000-000000000001', 'test owner');
insert into identity.tenants (id, name, slug) values
 ('41a00000-0000-4000-8000-000000000011', 'Features A', 'features-a'),
 ('41a00000-0000-4000-8000-000000000012', 'Features B', 'features-b');

create temporary table feature_observations (name text primary key, passed boolean);
grant insert on feature_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
select platform.admin_set_features('41a00000-0000-4000-8000-000000000001', '41a00000-0000-4000-8000-000000000021',
  '41a00000-0000-4000-8000-000000000011', '{"weapon": true, "loitering": false}'::jsonb);
insert into feature_observations values ('owner_lists',
  (select count(*) = 2 from platform.admin_list_features('41a00000-0000-4000-8000-000000000001')
   where tenant_id = '41a00000-0000-4000-8000-000000000011'));
do $$ begin
  begin
    perform platform.admin_set_features('41a00000-0000-4000-8000-000000000002', '41a00000-0000-4000-8000-000000000022',
      '41a00000-0000-4000-8000-000000000011', '{"weapon": false}'::jsonb);
    insert into feature_observations values ('client_refused', false);
  exception when insufficient_privilege then
    insert into feature_observations values ('client_refused', true);
  end;
  begin
    perform platform.admin_set_features('41a00000-0000-4000-8000-000000000001', '41a00000-0000-4000-8000-000000000023',
      '41a00000-0000-4000-8000-000000000011', '{"teleport": true}'::jsonb);
    insert into feature_observations values ('unknown_key', false);
  exception when sqlstate '22023' then
    insert into feature_observations values ('unknown_key', true);
  end;
  begin
    perform platform.admin_set_features('41a00000-0000-4000-8000-000000000001', '41a00000-0000-4000-8000-000000000024',
      '41a00000-0000-4000-8000-000000000011', '{"weapon": "yes"}'::jsonb);
    insert into feature_observations values ('bad_value', false);
  exception when sqlstate '22023' then
    insert into feature_observations values ('bad_value', true);
  end;
end $$;
set local app.tenant_id = '41a00000-0000-4000-8000-000000000011';
insert into config.feature_settings (tenant_id, feature, enabled, camera_ids, updated_by)
  values ('41a00000-0000-4000-8000-000000000011', 'weapon', true, array['41a00000-0000-4000-8000-000000000031']::uuid[], 'user:test');
insert into feature_observations values ('own_switch', (select count(*) = 1 from config.feature_settings));
do $$ begin
  begin
    insert into config.feature_settings (tenant_id, feature, enabled, updated_by)
      values ('41a00000-0000-4000-8000-000000000012', 'weapon', true, 'user:test');
    insert into feature_observations values ('foreign_switch', false);
  exception when insufficient_privilege then
    insert into feature_observations values ('foreign_switch', true);
  end;
  begin
    insert into identity.tenant_features (tenant_id, feature, included)
      values ('41a00000-0000-4000-8000-000000000011', 'zone_alerts', true);
    insert into feature_observations values ('client_plan_write', false);
  exception when insufficient_privilege then
    insert into feature_observations values ('client_plan_write', true);
  end;
end $$;
reset role;

select ok((select passed from feature_observations where name = 'owner_lists'), 'an operator sees which features a plan includes');
select ok((select passed from feature_observations where name = 'client_refused'), 'anyone else is refused plan changes');
select ok((select passed from feature_observations where name = 'unknown_key'), 'unknown features are refused');
select ok((select passed from feature_observations where name = 'bad_value'), 'non-boolean values are refused');
select ok((select passed from feature_observations where name = 'own_switch'), 'a workspace can set its own switches');
select ok((select passed from feature_observations where name = 'foreign_switch'), 'nobody can set another workspace''s switches');
select ok((select passed from feature_observations where name = 'client_plan_write'), 'the API cannot change a plan directly');
select is((select count(*)::int from audit.events where tenant_id = '41a00000-0000-4000-8000-000000000011'
  and action = 'tenant.features.changed' and actor_id = 'operator:41a00000-0000-4000-8000-000000000001'
  and before_state = '{}'::jsonb and after_state = '{"weapon": true, "loitering": false}'::jsonb
  and canonical_payload = convert_from(canonical_payload_bytes, 'UTF8')::jsonb
  and record_hash = extensions.digest(previous_hash || extensions.digest(canonical_payload_bytes, 'sha256')
    || convert_to(canonical_payload->>'occurred_at', 'UTF8'), 'sha256')),
  1, 'the plan change is recorded once in the workspace''s hash-chained audit log');
select ok(exists (select 1 from pg_constraint where conrelid = 'platform.site_keys'::regclass
  and contype = 'c' and pg_get_constraintdef(oid) like '%jsonb_typeof(capabilities)%'),
  'site-box capabilities must be a list');

select * from finish();
rollback;
