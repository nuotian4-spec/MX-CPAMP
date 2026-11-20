export interface PluginImportPreview {
  id: string;
  plugin_id: string;
  version: string;
  platform: string;
  library_path: string;
  license_required: boolean;
  license_status?: 'required' | 'not_required' | 'unknown';
  current_version: string;
  currently_enabled: boolean;
  compatibility: 'verified' | 'unverified' | 'incompatible';
  installable: boolean;
  blockers: string[];
}

export type UpdateOperationStatus =
  | 'queued'
  | 'installing'
  | 'reloading'
  | 'completed'
  | 'completed_disabled'
  | 'failed'
  | 'license_unverified'
  | 'license_error'
  | 'awaiting_restart'
  | 'recovery_required';

export interface UpdateOperation {
  plugin_id?: string;
  action?: string;
  id: string;
  type: string;
  target_version: string;
  status: UpdateOperationStatus;
  stage: string;
  active_version: string;
  registered: boolean;
  license_status: string;
  message: string;
  started_at: string;
  finished_at?: string;
}
