#!/bin/bash

# Stress Test Runner for mis2git
# This script runs comprehensive benchmarks to test system capacity

set -e

echo "🚀 Starting mis2git stress tests..."
echo "======================================"

# Create output directory
OUTPUT_DIR="stress_test_results"
mkdir -p $OUTPUT_DIR
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

echo "📊 Running file parsing benchmarks..."
echo "--------------------------------------"
go test -bench=BenchmarkNoteFileParsing -benchmem -timeout=10m ./internal/telegram >$OUTPUT_DIR/file_parsing_$TIMESTAMP.txt 2>&1
echo "✅ Note file parsing benchmark completed"

go test -bench=BenchmarkIssueFileParsing -benchmem -timeout=10m ./internal/telegram >>$OUTPUT_DIR/file_parsing_$TIMESTAMP.txt 2>&1
echo "✅ Issue file parsing benchmark completed"

go test -bench=BenchmarkTodoFileParsing -benchmem -timeout=10m ./internal/telegram >>$OUTPUT_DIR/file_parsing_$TIMESTAMP.txt 2>&1
echo "✅ Todo file parsing benchmark completed"

echo ""
echo "🔄 Running sync operation benchmarks..."
echo "---------------------------------------"
go test -bench=BenchmarkSyncOperations -benchmem -timeout=15m ./internal/telegram >$OUTPUT_DIR/sync_operations_$TIMESTAMP.txt 2>&1
echo "✅ Sync operations benchmark completed"

go test -bench=BenchmarkSyncCommand -benchmem -timeout=15m ./internal/telegram >$OUTPUT_DIR/sync_command_$TIMESTAMP.txt 2>&1
echo "✅ Sync command benchmark completed"

echo ""
echo "📝 Running issue command benchmarks..."
echo "--------------------------------------"
go test -bench=BenchmarkIssueCommand -benchmem -timeout=10m ./internal/telegram >$OUTPUT_DIR/issue_command_$TIMESTAMP.txt 2>&1
echo "✅ Issue command benchmark completed"

echo ""
echo "🧠 Running memory usage benchmarks..."
echo "-------------------------------------"
go test -bench=BenchmarkMemoryUsage -benchmem -timeout=10m ./internal/telegram >$OUTPUT_DIR/memory_usage_$TIMESTAMP.txt 2>&1
echo "✅ Memory usage benchmark completed"

go test -bench=BenchmarkMemoryUsageCommands -benchmem -timeout=10m ./internal/telegram >>$OUTPUT_DIR/memory_usage_$TIMESTAMP.txt 2>&1
echo "✅ Command memory usage benchmark completed"

echo ""
echo "⚡ Running concurrent operation benchmarks..."
echo "--------------------------------------------"
go test -bench=BenchmarkConcurrentOperations -benchmem -timeout=15m ./internal/telegram >$OUTPUT_DIR/concurrent_ops_$TIMESTAMP.txt 2>&1
echo "✅ Concurrent operations benchmark completed"

go test -bench=BenchmarkConcurrentCommands -benchmem -timeout=15m ./internal/telegram >>$OUTPUT_DIR/concurrent_ops_$TIMESTAMP.txt 2>&1
echo "✅ Concurrent commands benchmark completed"

echo ""
echo "📈 Running content generation benchmarks..."
echo "-------------------------------------------"
go test -bench=BenchmarkContentGeneration -benchmem -timeout=10m ./internal/telegram >$OUTPUT_DIR/content_generation_$TIMESTAMP.txt 2>&1
echo "✅ Content generation benchmark completed"

echo ""
echo "🏭 Running database operation benchmarks..."
echo "-------------------------------------------"
go test -bench=BenchmarkDatabaseOperations -benchmem -timeout=10m ./internal/telegram >$OUTPUT_DIR/database_ops_$TIMESTAMP.txt 2>&1
echo "✅ Database operations benchmark completed"

echo ""
echo "🧪 Running real-world scenario tests..."
echo "---------------------------------------"
go test -run=TestRealWorldScenarios -timeout=20m ./internal/telegram >$OUTPUT_DIR/real_world_scenarios_$TIMESTAMP.txt 2>&1
echo "✅ Real-world scenarios test completed"

echo ""
echo "⚖️ Running performance threshold tests..."
echo "-----------------------------------------"
go test -run=TestPerformanceThresholds -timeout=10m ./internal/telegram >$OUTPUT_DIR/performance_thresholds_$TIMESTAMP.txt 2>&1
echo "✅ Performance threshold tests completed"

echo ""
echo "📏 Running file size limit tests..."
echo "-----------------------------------"
go test -run=TestFileSizeLimits -timeout=10m ./internal/telegram >$OUTPUT_DIR/file_size_limits_$TIMESTAMP.txt 2>&1
echo "✅ File size limit tests completed"

echo ""
echo "🎯 Running issue processing scalability tests..."
echo "------------------------------------------------"
go test -run=TestIssueProcessingScalability -timeout=20m ./internal/telegram >$OUTPUT_DIR/issue_scalability_$TIMESTAMP.txt 2>&1
echo "✅ Issue processing scalability tests completed"

echo ""
echo "📋 Generating summary report..."
echo "------------------------------"

# Create summary report
SUMMARY_FILE="$OUTPUT_DIR/stress_test_summary_$TIMESTAMP.md"

cat >$SUMMARY_FILE <<EOF
# Stress Test Summary Report

**Generated:** $(date)
**Test Run ID:** $TIMESTAMP

## Test Categories Completed

✅ **File Parsing Benchmarks**
- Note.md files (1KB to 10MB)
- Issue.md parsing (10 to 5000 issues)  
- Todo.md parsing (50 to 2000 todos)

✅ **Sync Operation Benchmarks**
- Mock API delays (100ms to 2s)
- Issue counts (10 to 1000)
- Command-level sync testing

✅ **Issue Command Benchmarks**
- Dataset sizes (10 to 5000 issues)
- Pagination testing
- Offset performance

✅ **Memory Usage Benchmarks**
- File sizes up to 10MB
- Command memory allocation tracking
- Large dataset processing

✅ **Concurrent Operation Benchmarks**
- Multiple goroutines (5 to 20)
- Concurrent user simulation
- Resource contention testing

✅ **Content Generation Benchmarks**
- Issue content generation (10 to 5000 issues)
- Template processing performance

✅ **Database Operation Benchmarks**
- User scaling (10 to 1000 users)
- Query performance simulation

✅ **Real-World Scenario Tests**
- Small repo + fast connection
- Medium repo + normal connection  
- Large repo + slow connection
- Enterprise repo scenarios

✅ **Performance Threshold Tests**
- Parse time thresholds
- Generation time limits
- Memory allocation limits

✅ **File Size Limit Tests**
- Processing time scaling
- Performance degradation points

✅ **Issue Processing Scalability Tests**
- API delay impact analysis
- Throughput measurements
- Scalability bottlenecks

## Key Metrics Measured

- **Processing Time**: How long operations take
- **Memory Allocation**: Peak memory usage and allocation patterns
- **Throughput**: Items processed per second
- **Concurrency**: Performance under concurrent load
- **Scalability**: Performance degradation with size

## Files Generated

EOF

# List all generated files
for file in $OUTPUT_DIR/*_$TIMESTAMP.txt; do
  if [ -f "$file" ]; then
    echo "- \`$(basename $file)\`" >>$SUMMARY_FILE
  fi
done

cat >>$SUMMARY_FILE <<EOF

## How to Analyze Results

1. **Look for performance degradation patterns** in file_parsing results
2. **Check sync operation scaling** in sync_operations and sync_command files
3. **Analyze memory growth** in memory_usage files  
4. **Review concurrent performance** in concurrent_ops files
5. **Examine real-world scenarios** for practical insights
6. **Check threshold violations** in performance_thresholds file

## Recommended Actions

Based on the results, consider:

- Setting file size limits based on processing time thresholds
- Implementing pagination for large issue lists
- Adding caching for frequently accessed large files
- Setting up monitoring for performance degradation
- Optimizing memory usage for large datasets

EOF

echo "📊 All stress tests completed successfully!"
echo ""
echo "📁 Results saved to: $OUTPUT_DIR/"
echo "📋 Summary report: $SUMMARY_FILE"
echo ""
echo "🔍 Next steps:"
echo "1. Review the summary report for key insights"
echo "2. Check individual benchmark files for detailed metrics"
echo "3. Look for performance bottlenecks and scaling issues"
echo "4. Consider implementing optimizations based on findings"
echo ""
echo "💡 Example commands to analyze results:"
echo "   cat $SUMMARY_FILE"
echo "   grep -E '(B/op|allocs/op)' $OUTPUT_DIR/*_$TIMESTAMP.txt"
echo "   grep -E '(WARNING|exceeded)' $OUTPUT_DIR/*_$TIMESTAMP.txt"

