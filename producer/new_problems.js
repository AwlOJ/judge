// Bước 1: Tạo một đề bài (problem) trong collection 'problems'
// Ghi lại _id được tạo ra sau khi chạy lệnh này để sử dụng cho bước tiếp theo.
const problemResult = db.problems.insertOne({
  title: "Sum of Two Numbers",
  description: "Write a program that reads two integers A and B, and prints their sum.",
  timeLimit: 1, // Giới hạn thời gian (giây)
  memoryLimit: 256, // Giới hạn bộ nhớ (MB)
  testCases: [
    { input: "1 2", output: "3" },
    { input: "5 7", output: "12" },
    { input: "100 200", output: "300" }
  ],
  createdAt: new Date(),
  updatedAt: new Date()
});

print("Inserted Problem ID:", problemResult.insertedId);
// Ví dụ: Inserted Problem ID: ObjectId("65c010c2a7b8c9d0e1f2g3h4")
// Bạn CẦN thay thế ObjectId("...") này bằng ID thực tế được in ra.
