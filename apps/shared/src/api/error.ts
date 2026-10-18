// What a call answers when it fails: the status and the words of the
// server, or of the mock, and the code of a refusal the screens act on
// ("github_link_required").
export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}
