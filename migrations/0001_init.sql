-- ============================================================================
-- RankPulse — Initial Schema Migration
-- Run this in the Supabase SQL Editor (or via `supabase db push`)
-- ============================================================================

create extension if not exists "uuid-ossp";

-- ========== USERS ==========
-- Extends Supabase's built-in auth.users with billing/plan info.
create table public.users (
    id uuid primary key references auth.users(id) on delete cascade,
    email text not null unique,
    paddle_customer_id text unique,
    subscription_status text not null default 'inactive'
        check (subscription_status in ('active', 'canceled', 'past_due', 'inactive')),
    plan_type text check (plan_type in ('monthly', 'annual')),
    created_at timestamptz not null default now()
);

-- ========== PROJECTS ==========
create table public.projects (
    id uuid primary key default uuid_generate_v4(),
    user_id uuid not null references public.users(id) on delete cascade,
    domain text not null,
    target_country text not null default 'us',
    created_at timestamptz not null default now()
);

create index idx_projects_user_id on public.projects(user_id);

-- ========== KEYWORDS ==========
create table public.keywords (
    id uuid primary key default uuid_generate_v4(),
    project_id uuid not null references public.projects(id) on delete cascade,
    keyword_text text not null,
    current_position int,
    previous_position int,
    last_checked_at timestamptz,
    created_at timestamptz not null default now(),
    unique (project_id, keyword_text)
);

create index idx_keywords_project_id on public.keywords(project_id);
create index idx_keywords_last_checked on public.keywords(last_checked_at);

-- ========== RANK HISTORY ==========
create table public.rank_history (
    id bigserial primary key,
    keyword_id uuid not null references public.keywords(id) on delete cascade,
    position int,
    checked_date date not null default current_date,
    unique (keyword_id, checked_date)
);

create index idx_rank_history_keyword_id on public.rank_history(keyword_id);

-- ========== AUTO-CREATE public.users ON SIGNUP ==========
-- Keeps public.users in sync with auth.users automatically.
create or replace function public.handle_new_user()
returns trigger as $$
begin
    insert into public.users (id, email)
    values (new.id, new.email)
    on conflict (id) do nothing;
    return new;
end;
$$ language plpgsql security definer;

create trigger on_auth_user_created
    after insert on auth.users
    for each row execute procedure public.handle_new_user();

-- ========== ROW LEVEL SECURITY ==========
alter table public.users enable row level security;
alter table public.projects enable row level security;
alter table public.keywords enable row level security;
alter table public.rank_history enable row level security;

create policy "Users can view own record" on public.users
    for select using (auth.uid() = id);

create policy "Users can update own record" on public.users
    for update using (auth.uid() = id);

create policy "Users manage own projects" on public.projects
    for all using (auth.uid() = user_id);

create policy "Users manage own keywords" on public.keywords
    for all using (
        exists (
            select 1 from public.projects p
            where p.id = keywords.project_id and p.user_id = auth.uid()
        )
    );

create policy "Users view own rank history" on public.rank_history
    for select using (
        exists (
            select 1 from public.keywords k
            join public.projects p on p.id = k.project_id
            where k.id = rank_history.keyword_id and p.user_id = auth.uid()
        )
    );

-- NOTE: The Go worker connects using the Supabase service_role key,
-- which bypasses RLS by design — this is required for the daily cron
-- job to read/write across all users' keywords.
