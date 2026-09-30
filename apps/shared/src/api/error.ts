// What a call answers when it fails: the status and the words of the
// server, or of the mock.
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}
