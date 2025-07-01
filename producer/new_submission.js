db.submissions.insertOne({
  userId: new ObjectId(),
  problemId: new ObjectId("6863db1753b521fcc6ef05bc"), 
  code: `#include <iostream>\nint main() { int a, b; std::cin >> a >> b; std::cout << a + b << std::endl; return 0; }`,
  language: "cpp",
  status: "In Queue",
  createdAt: new Date(),
  updatedAt: new Date()
});
