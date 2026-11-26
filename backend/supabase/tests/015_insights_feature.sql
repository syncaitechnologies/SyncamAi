-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(4);

insert into auth.users (id, email) values ('41b00000-0000-4000-8000-000000000001', 'owner@example.test');
insert into platform.operators (user_id, note) values ('41b00000-0000-4000-8000-000000000001', 'test owner');
insert into identity.tenants (id, name, slug) values ('41b00000-0000-4000-8000-000000000011', 'Insights A', 'insights-a');

create temporary table insights_observations (name text primary key, passed boolean);
grant insert on insights_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
select platform.admin_set_features('41b00000-0000-4000-8000-000000000001', '41b00000-0000-4000-8000-000000000021',
  '41b00000-0000-4000-8000-000000000011', '{"insights": true}'::jsonb);
set local app.tenant_id = '41b00000-0000-4000-8000-000000000011';
insert into config.feature_settings (tenant_id, feature, enabled, updated_by)
  values ('41b00000-0000-4000-8000-000000000011', 'insights', false, 'user:test');
insert into insights_observations values ('switch', (select count(*) = 1 from config.feature_settings where feature = 'insights'));
reset role;

select ok((select included from identity.tenant_features
  where tenant_id = '41b00000-0000-4000-8000-000000000011' and feature = 'insights'),
  'the owner can add insights to a plan');
select is((select count(*)::int from audit.events where tenant_id = '41b00000-0000-4000-8000-000000000011'
  and action = 'tenant.features.changed' and after_state = '{"insights": true}'::jsonb),
  1, 'adding insights is recorded in the audit log');
select ok((select passed from insights_observations where name = 'switch'), 'a workspace can switch insights off');
select throws_ok($$insert into identity.tenant_features (tenant_id, feature, included)
  values ('41b00000-0000-4000-8000-000000000011', 'teleport', true)$$, '23514', null, 'unknown features are still refused');

select * from finish();
rollback;
