-- Disposable CI database only. Every fixture is rolled back.
begin;
select plan(3);

insert into identity.tenants (id, name, slug) values
 ('41d00000-0000-4000-8000-000000000001', 'Box version test', 'box-version-test');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41d00000-0000-4000-8000-000000000011', '41d00000-0000-4000-8000-000000000001', 'Cafe', 'Asia/Kolkata', 'test');
insert into platform.site_keys (id, tenant_id, site_id, label, key_hash, created_by) values
 ('41d00000-0000-4000-8000-000000000021', '41d00000-0000-4000-8000-000000000001', '41d00000-0000-4000-8000-000000000011',
  'Front PC', repeat('d', 64), 'test');

select is((select box_version from platform.site_keys where id = '41d00000-0000-4000-8000-000000000021'),
  null, 'a box that has not reported has no version');
select lives_ok($$update platform.site_keys set box_version = '0.1.9' where id = '41d00000-0000-4000-8000-000000000021'$$,
  'a plain version is stored');
select throws_ok($$update platform.site_keys set box_version = '0.1.9<script>' where id = '41d00000-0000-4000-8000-000000000021'$$,
  '23514', null, 'anything else is refused');

select * from finish();
rollback;
