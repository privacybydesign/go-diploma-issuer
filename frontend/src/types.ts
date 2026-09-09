export interface CheckInfo {
  id: string;
  name: string;
  ok: boolean;
  detail: string;
}

export interface ValidationInfo {
  valid: boolean;
  /** "valid" or the id of the first failed check. */
  key: string;
  signer?: string;
  trust_anchor?: string;
  signing_time: string;
  timestamp_trusted: boolean;
  checks: CheckInfo[];
}

export interface DocumentInfo {
  document_type: string;
  qualification: string;
  profiles: string[];
  full_name: string;
  date_of_birth: string;
  institution: string;
  place_of_issue: string;
  date_awarded: string;
  nlqf_level: string;
  eqf_level: string;
  document_number: string;
  download_date: string;
  pages: number;
  has_grade_list: boolean;
}

export interface FileResult {
  filename: string;
  accepted: boolean;
  error?: string;
  message?: string;
  validation?: ValidationInfo;
  document?: DocumentInfo;
}

export interface PersonInfo {
  full_name: string;
  date_of_birth: string;
}

export interface UploadResponse {
  session_id: string;
  person: PersonInfo;
  files: FileResult[];
  accepted: number;
  rejected: number;
}

export interface IdentityMatchInfo {
  source: string;
  matched: boolean;
  date_of_birth_match: boolean;
  surname_match: boolean;
  given_names_match: boolean;
  reasons?: string[];
}

export interface IssuanceResponse {
  jwt: string;
  irma_server_url: string;
  credentials: number;
  identity: IdentityMatchInfo;
}

export interface ErrorResponse {
  error: string;
  message?: string;
  files?: FileResult[];
  identity?: IdentityMatchInfo;
}
